import { DBOS, WorkflowContext } from "@dbos-inc/dbos-sdk";

export class SampleApp {
  @DBOS.workflow()
  static async helloWorkflow(ctxt: WorkflowContext, name: string): Promise<string> {
    return `Hello, ${name}!`;
  }
}
