CREATE DATABASE relay_golang;
CREATE DATABASE relay_python;
CREATE DATABASE relay_typescript;
CREATE DATABASE relay_java;

\c relay_golang;
CREATE TABLE test_step_executions (
    workflow_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

\c relay_python;
CREATE TABLE test_step_executions (
    workflow_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

\c relay_typescript;
CREATE TABLE test_step_executions (
    workflow_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

\c relay_java;
CREATE TABLE test_step_executions (
    workflow_id TEXT NOT NULL,
    step_name TEXT NOT NULL,
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
