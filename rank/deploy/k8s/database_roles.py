"""Provision schema owners and restricted runtime logins in the dedicated lab database."""
import base64
import hashlib
import hmac
import json
import os
import re
import secrets
import subprocess

from lab import KUBE, STATE, apply_json


REVISION = 'roles1'
SCHEMAS = {'execution': ['execution_schema', 'river_migration'], 'rank': ['schema_migrations']}


def credentials():
    """Keep stable login passwords outside Git; never return them in command output."""
    path = STATE / 'database-roles.json'
    if not path.exists():
        data = {schema + '_' + purpose: secrets.token_hex(32) for schema in SCHEMAS for purpose in ['migrator', 'runtime']}
        with os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w') as file:
            json.dump(data, file)
    if path.stat().st_mode & 0o077:
        raise RuntimeError('Database credential file must be owner-only')
    data = json.loads(path.read_text())
    expected = {schema + '_' + purpose for schema in SCHEMAS for purpose in ['migrator', 'runtime']}
    if set(data) != expected or any(not re.fullmatch('[a-f0-9]{64}', value) for value in data.values()):
        raise RuntimeError('Invalid lab database credential file')
    return data


def database_url(role):
    return 'postgres://' + role + ':' + credentials()[role] + '@postgres:5432/execution?sslmode=disable'


def admin_sql(sql):
    """Capture diagnostics because PostgreSQL errors may include credential-bearing SQL."""
    result = subprocess.run(KUBE + ['-n', 'rank-dev', 'exec', '-i', 'deployment/postgres', '--',
        'psql', '-X', '-qAt', '-U', 'execution', '-d', 'execution', '-v', 'ON_ERROR_STOP=1'],
        input=sql, text=True, capture_output=True, timeout=45)
    if result.returncode:
        raise RuntimeError('Lab database permission operation failed; SQL diagnostics suppressed')
    return result.stdout


def scram(password):
    """Pass a SCRAM verifier, never a plaintext password, to PostgreSQL DDL."""
    salt = secrets.token_bytes(16)
    salted = hashlib.pbkdf2_hmac('sha256', password.encode(), salt, 4096)
    stored = hashlib.sha256(hmac.digest(salted, b'Client Key', 'sha256')).digest()
    server = hmac.digest(salted, b'Server Key', 'sha256')
    encode = lambda value: base64.b64encode(value).decode()
    return 'SCRAM-SHA-256$4096:' + encode(salt) + '$' + encode(stored) + ':' + encode(server)


def provision():
    """Transfer only the two application schemas while all application writers are stopped."""
    statements = ['BEGIN;', 'REVOKE ALL ON DATABASE execution FROM PUBLIC;', 'REVOKE ALL ON SCHEMA public FROM PUBLIC;']
    for role, password in credentials().items():
        statements.append(f"""
DO $$ BEGIN
  IF EXISTS (SELECT 1 FROM pg_auth_members WHERE member=(SELECT oid FROM pg_roles WHERE rolname='{role}')) THEN
    RAISE EXCEPTION 'Unexpected role membership';
  END IF;
  IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname='{role}') THEN CREATE ROLE {role}; END IF;
END $$;
ALTER ROLE {role} LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOINHERIT NOREPLICATION NOBYPASSRLS PASSWORD '{scram(password)}';
REVOKE ALL ON DATABASE execution FROM {role};
GRANT CONNECT ON DATABASE execution TO {role};
""")
    for schema in SCHEMAS:
        owner, runtime = schema + '_migrator', schema + '_runtime'
        statements.append(f"""
GRANT CREATE ON DATABASE execution TO {owner};
CREATE SCHEMA IF NOT EXISTS {schema} AUTHORIZATION {owner};
ALTER SCHEMA {schema} OWNER TO {owner};
REVOKE ALL ON SCHEMA {schema} FROM PUBLIC, rank_runtime, execution_runtime, rank_migrator, execution_migrator;
GRANT ALL ON SCHEMA {schema} TO {owner};
GRANT USAGE ON SCHEMA {schema} TO {runtime};
DO $$ DECLARE obj record; BEGIN
  FOR obj IN SELECT c.oid::regclass AS name FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
    WHERE n.nspname='{schema}' AND c.relkind IN ('r','p')
  LOOP EXECUTE format('ALTER TABLE %s OWNER TO {owner}', obj.name); END LOOP;
  FOR obj IN SELECT c.oid::regclass AS name FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
    WHERE n.nspname='{schema}' AND c.relkind='S'
  LOOP EXECUTE format('ALTER SEQUENCE %s OWNER TO {owner}', obj.name); END LOOP;
  FOR obj IN SELECT p.oid::regprocedure AS name FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='{schema}'
  LOOP EXECUTE format('ALTER ROUTINE %s OWNER TO {owner}', obj.name); END LOOP;
  FOR obj IN SELECT t.oid::regtype AS name FROM pg_type t JOIN pg_namespace n ON n.oid=t.typnamespace WHERE n.nspname='{schema}' AND t.typtype IN ('e','d')
  LOOP
    EXECUTE format('ALTER TYPE %s OWNER TO {owner}', obj.name);
    EXECUTE format('REVOKE ALL ON TYPE %s FROM PUBLIC', obj.name);
    EXECUTE format('GRANT USAGE ON TYPE %s TO {runtime}', obj.name);
  END LOOP;
END $$;
ALTER DEFAULT PRIVILEGES FOR ROLE {owner} REVOKE EXECUTE ON FUNCTIONS FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE {owner} REVOKE USAGE ON TYPES FROM PUBLIC;
ALTER DEFAULT PRIVILEGES FOR ROLE {owner} IN SCHEMA {schema} GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO {runtime};
ALTER DEFAULT PRIVILEGES FOR ROLE {owner} IN SCHEMA {schema} GRANT USAGE, SELECT ON SEQUENCES TO {runtime};
ALTER DEFAULT PRIVILEGES FOR ROLE {owner} IN SCHEMA {schema} GRANT EXECUTE ON FUNCTIONS TO {runtime};
ALTER DEFAULT PRIVILEGES FOR ROLE {owner} IN SCHEMA {schema} GRANT USAGE ON TYPES TO {runtime};
""")
    admin_sql('\n'.join(statements) + '\nCOMMIT;')
    for schema in SCHEMAS:
        key = schema.upper() + '_DATABASE_URL'
        apply_json({'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': schema + '-migration', 'namespace': 'rank-dev'},
                    'stringData': {key: database_url(schema + '_migrator')}})


def grant_runtime():
    """Apply DML grants after migrations and retain read-only schema-version metadata."""
    statements = ['BEGIN;']
    for schema, metadata in SCHEMAS.items():
        runtime = schema + '_runtime'
        statements.append(f"""
REVOKE ALL ON ALL TABLES IN SCHEMA {schema} FROM PUBLIC, rank_runtime, execution_runtime;
REVOKE ALL ON ALL SEQUENCES IN SCHEMA {schema} FROM PUBLIC, rank_runtime, execution_runtime;
REVOKE ALL ON ALL ROUTINES IN SCHEMA {schema} FROM PUBLIC, rank_runtime, execution_runtime;
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA {schema} TO {runtime};
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA {schema} TO {runtime};
GRANT EXECUTE ON ALL ROUTINES IN SCHEMA {schema} TO {runtime};
""")
        for table in metadata:
            statements.append(f"""
DO $$ BEGIN
  IF to_regclass('{schema}.{table}') IS NOT NULL THEN
    REVOKE ALL ON TABLE {schema}.{table} FROM {runtime};
    GRANT SELECT ON TABLE {schema}.{table} TO {runtime};
  END IF;
END $$;
""")
    admin_sql('\n'.join(statements) + '\nCOMMIT;')
