import { DBOS, DBOSSerializer } from "@dbos-inc/dbos-sdk";

const appName = process.env.DBOS_APP_NAME!;
const systemDatabaseUrl = process.env.DBOS_SYSTEM_DATABASE_URL!;
const executorID = process.env.DBOS__VMID || "typescript-direct-fixture";

const portableJSON: DBOSSerializer = {
  name: () => "portable_json",
  stringify: (value) => JSON.stringify(value ?? null),
  parse: (text) =>
    text === null || text === undefined || text === "" ? null : JSON.parse(text),
};

DBOS.setConfig({
  name: appName,
  systemDatabaseUrl,
  executorID,
  serializer: portableJSON,
});

let resolveBlockingStarted: () => void = () => {};
const blockingStarted = new Promise<void>((resolve) => {
  resolveBlockingStarted = resolve;
});

class DirectFixture {
  @DBOS.step()
  static async fixtureStep(): Promise<string> {
    return "step-ok";
  }

  @DBOS.step()
  static async checkpointStep(): Promise<string> {
    return "checkpoint";
  }

  @DBOS.step()
  static async blockingStartStep(): Promise<string> {
    resolveBlockingStarted();
    return "started";
  }

  @DBOS.workflow()
  static async fixtureWorkflow(name: string): Promise<string> {
    await DirectFixture.fixtureStep();
    return `hello ${name}`;
  }

  @DBOS.workflow()
  static async blockingWorkflow(): Promise<string> {
    await DirectFixture.checkpointStep();
    await DirectFixture.blockingStartStep();
    await new Promise((resolve) => setTimeout(resolve, 30_000));
    return "done";
  }
}

async function withTimeout<T>(promise: Promise<T>, milliseconds: number, message: string): Promise<T> {
  let timer: NodeJS.Timeout | undefined;
  const timeout = new Promise<T>((_, reject) => {
    timer = setTimeout(() => reject(new Error(message)), milliseconds);
  });
  try {
    return await Promise.race([promise, timeout]);
  } finally {
    if (timer) {
      clearTimeout(timer);
    }
  }
}

async function main(): Promise<void> {
  await DBOS.launch();
  const completed = await DBOS.startWorkflow(DirectFixture.fixtureWorkflow)("typescript");
  await completed.getResult();
  const blocking = await DBOS.startWorkflow(DirectFixture.blockingWorkflow)();
  await withTimeout(blockingStarted, 15_000, "blocking workflow did not start");
  const line = JSON.stringify({
    completed_workflow_id: completed.workflowID,
    blocking_workflow_id: blocking.workflowID,
    executor_id: executorID,
  });
  process.stdout.write(line + "\n", () => process.exit(0));
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
