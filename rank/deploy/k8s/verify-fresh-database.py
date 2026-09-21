"""Migrate a disposable empty database with the packaged production binaries."""
from datetime import datetime, timezone
import json
import subprocess
import uuid
from lab import KUBE, STATE, apply_json
from database_roles import admin_sql, database_url, credentials, SCHEMAS
from verify import kubectl


def main():
    database = 'rank_fresh_' + uuid.uuid4().hex[:12]
    name = 'rank-fresh-' + uuid.uuid4().hex[:12]
    image = json.loads((STATE / 'platform-images.json').read_text())['images']['platform']
    report = {'startedAt': datetime.now(timezone.utc).isoformat(), 'database': database, 'image': image}
    admin_sql('CREATE DATABASE ' + database + ';')
    try:
        admin_sql('REVOKE ALL ON DATABASE ' + database + ' FROM PUBLIC;\n' + '\n'.join('GRANT CONNECT' + (', CREATE' if role.endswith('migrator') else '') + ' ON DATABASE ' + database + ' TO ' + role + ';' for role in credentials()))
        apply_json({'apiVersion': 'v1', 'kind': 'Secret', 'metadata': {'name': name, 'namespace': 'rank-dev'}, 'stringData': {
            schema.upper() + '_DATABASE_URL': database_url(schema + '_migrator').replace('/execution?', '/' + database + '?') for schema in SCHEMAS}})
        for run in range(2):
            job = name + '-' + str(run)
            containers = [{'name': schema, 'image': image, 'imagePullPolicy': 'Never', 'command': ['/usr/local/bin/' + ('executiond' if schema == 'execution' else 'rankd')], 'args': ['-migrate', '-database-timeout=45s'], 'envFrom': [{'secretRef': {'name': name}}]} for schema in SCHEMAS]
            apply_json({'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {'name': job, 'namespace': 'rank-dev'}, 'spec': {'backoffLimit': 0, 'activeDeadlineSeconds': 90, 'template': {'spec': {'restartPolicy': 'Never', 'automountServiceAccountToken': False, 'initContainers': containers[:1], 'containers': containers[1:]}}}})
            kubectl('wait', '--for=condition=complete', 'job/' + job, '--timeout=60s')
        def query(sql):
            result = subprocess.run(KUBE + ['-n', 'rank-dev', 'exec', '-i', 'deployment/postgres', '--', 'psql', '-XqAt', '-U', 'execution', '-d', database, '-v', 'ON_ERROR_STOP=1'], input=sql, text=True, capture_output=True, timeout=30)
            if result.returncode: raise RuntimeError('Fresh-database verification failed; diagnostics suppressed')
            return result.stdout.strip()
        report['schemas'] = query("SELECT string_agg(nspname, ',' ORDER BY nspname) FROM pg_namespace WHERE nspname IN ('rank','execution');")
        assert report['schemas'] == 'execution,rank'
        report['migrationOwners'] = query("SELECT string_agg(DISTINCT tableowner, ',' ORDER BY tableowner) FROM pg_tables WHERE schemaname IN ('rank','execution');")
        assert report['migrationOwners'] == 'execution_migrator,rank_migrator'
        assert query('SELECT count(*) FROM execution.executions;') == '0'
        assert query('SELECT count(*) FROM rank.conversations;') == '0'
        assert query("SELECT to_regclass('execution.model_permits') IS NOT NULL AND to_regclass('execution.river_job') IS NOT NULL;") == 't'
        report.update(passed=True, repeatedMigrationPreservesEmptyState=True, modelQuotaAndRiverTablesPresent=True)
    finally:
        for run in range(2): kubectl('delete', 'job/' + name + '-' + str(run), '--ignore-not-found', '--wait=true', '--timeout=30s')
        kubectl('delete', 'secret/' + name, '--ignore-not-found')
        admin_sql('DROP DATABASE ' + database + ' WITH (FORCE);')
        report['endedAt'] = datetime.now(timezone.utc).isoformat()
        path = STATE / (name + '.json'); path.write_text(json.dumps(report, indent=2) + '\n')
        print('Fresh database evidence:', path, 'passed:', report.get('passed', False))


if __name__ == '__main__': main()
