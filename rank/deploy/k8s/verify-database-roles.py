"""Verify real PostgreSQL logins, cross-schema denials and workload credential exposure."""
from datetime import datetime, timezone
import json
import subprocess
import uuid

from database_roles import SCHEMAS, admin_sql, credentials
from lab import HERE, KUBE, STATE, run


def query(role, sql, *, denied=False):
    # TCP forces password authentication; local socket trust would not verify credentials.
    script = 'export PGPASSWORD=' + credentials()[role] + '\nexec psql -X -qAt -h 127.0.0.1 -U ' + role + " -d execution -v ON_ERROR_STOP=1 -v VERBOSITY=sqlstate <<'RANK_SQL'\n" + sql + '\nRANK_SQL\n'
    result = subprocess.run(KUBE + ['-n', 'rank-dev', 'exec', '-i', 'deployment/postgres', '--', 'sh'],
                            input=script, text=True, capture_output=True, timeout=30)
    if denied:
        if result.returncode == 0 or '42501' not in result.stderr:
            raise RuntimeError('Expected insufficient_privilege for ' + role + '; diagnostics suppressed')
    elif result.returncode:
        raise RuntimeError('Database verification failed for ' + role + '; diagnostics suppressed')
    return result.stdout.strip()


def main():
    report = {'startedAt': datetime.now(timezone.utc).isoformat(), 'checks': [],
              'images': json.loads((STATE / 'platform-images.json').read_text())}
    probe = 'permission_probe_' + uuid.uuid4().hex
    for schema, metadata in SCHEMAS.items():
        owner, runtime = schema + '_migrator', schema + '_runtime'
        other = 'rank' if schema == 'execution' else 'execution'
        table = schema + '.' + probe
        assert query(runtime, 'SELECT current_user;') == runtime
        assert query(runtime, 'SELECT rolsuper OR rolcreatedb OR rolcreaterole OR rolreplication OR rolbypassrls FROM pg_roles WHERE rolname=current_user;') == 'f'
        protected = ['rank.conversations', 'rank.api_credentials'] if schema == 'execution' else ['execution.credentials', 'execution.executions']
        for target in protected:
            query(runtime, 'SELECT * FROM ' + target + ' LIMIT 0;', denied=True)
        query(owner, 'CREATE TABLE ' + table + ' (id bigserial PRIMARY KEY, value text NOT NULL);')
        try:
            assert query(runtime, 'INSERT INTO ' + table + " (value) VALUES ('probe') RETURNING id;") == '1'
            assert query(runtime, 'UPDATE ' + table + " SET value='updated' RETURNING value;") == 'updated'
            assert query(runtime, 'SELECT count(*) FROM ' + table + ';') == '1'
            assert query(runtime, 'DELETE FROM ' + table + ' RETURNING id;') == '1'
            for sql in [
                'SELECT * FROM ' + other + '.' + SCHEMAS[other][0] + ' LIMIT 0;',
                'SET ROLE ' + owner + ';',
                'CREATE SCHEMA rank_forbidden;',
                'CREATE TEMP TABLE rank_forbidden(id int);',
                'CREATE TABLE ' + schema + '.rank_forbidden(id int);',
                'ALTER TABLE ' + table + ' ADD COLUMN forbidden int;',
                'DROP TABLE ' + table + ';',
                'TRUNCATE ' + table + ';',
                "SELECT pg_read_file('/etc/passwd');",
                'CREATE ROLE rank_forbidden;',
            ]:
                query(runtime, 'BEGIN;\n' + sql + '\nROLLBACK;', denied=True)
            for version_table in metadata:
                query(runtime, 'SELECT * FROM ' + schema + '.' + version_table + ' LIMIT 0;')
                query(runtime, 'BEGIN; DELETE FROM ' + schema + '.' + version_table + ' WHERE false; ROLLBACK;', denied=True)
            query(owner, 'SELECT * FROM ' + other + '.' + SCHEMAS[other][0] + ' LIMIT 0;', denied=True)
            report['checks'].append({'schema': schema, 'authenticatedDML': True, 'futureTableDefaults': True,
                'crossSchemaDenied': True, 'migrationMetadataReadOnly': True, 'ddlAndPrivilegeEscalationDenied': True})
        finally:
            query(owner, 'DROP TABLE IF EXISTS ' + table + ';')
    deployments = json.loads(run(KUBE + ['-n', 'rank-dev', 'get', 'deployments', '-o', 'json'], capture=True))
    for item in deployments['items']:
        name = item['metadata']['name']
        if name not in ['rank-api', 'rank-dispatcher', 'rank-control', 'execution-api', 'execution-worker']:
            continue
        pod = item['spec']['template']['spec']
        assert pod['automountServiceAccountToken'] is False
        for container in pod['containers']:
            refs = [source['secretRef']['name'] for source in container.get('envFrom', []) if 'secretRef' in source]
            assert not any(ref.endswith('-migration') or ref == 'execution-lab' for ref in refs)
    # Inspect only environment key names inside the running control process container.
    keys = json.loads(run(KUBE + ['-n', 'rank-dev', 'exec', 'deployment/rank-control', '--', 'node', '-e',
                                'console.log(JSON.stringify(Object.keys(process.env)))'], capture=True))
    assert not any('DATABASE' in key or key.startswith('POSTGRES') or key == 'RANK_ACTION_SECRET' for key in keys)
    for schema in SCHEMAS:
        secret = json.loads(run(KUBE + ['-n', 'rank-dev', 'get', 'secret/' + schema + '-migration', '-o', 'json'], capture=True))
        assert list(secret['data']) == [schema.upper() + '_DATABASE_URL']
    active_roles = admin_sql("SELECT DISTINCT usename FROM pg_stat_activity WHERE datname='execution';").splitlines()
    assert all(schema + '_runtime' in active_roles for schema in SCHEMAS)
    report['checks'].append({'controlHasNoDatabaseOrActionSecret': True, 'migrationSecretsDatabaseOnly': True,
                              'applicationPodsHaveNoMigrationSecretOrServiceAccountToken': True})
    expected = json.loads((HERE / 'fixtures/database-roles.snapshot.json').read_text())
    assert report['checks'] == expected
    report['completedAt'] = datetime.now(timezone.utc).isoformat()
    path = STATE / ('database-roles-' + uuid.uuid4().hex + '.json')
    path.write_text(json.dumps(report, indent=2) + '\n')
    print('Database permission verification passed; report:', path)


if __name__ == '__main__':
    main()
