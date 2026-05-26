#!/usr/bin/env node
/**
 * Kilroy headless bridge around @cursor/sdk.
 * Emits Claude-compatible stream-json NDJSON on stdout for Kilroy's CLI parser.
 */
import { Agent } from "@cursor/sdk";
import * as fs from "node:fs";
import * as path from "node:path";

const HELP = `Usage: kilroy-cursor-agent run --cwd <dir> --model <id> [--stream-json] [--output-format json] [--agent-id <id>] [--interactive] [--append-system-prompt <text>]
       kilroy-cursor-agent --help

Local agent runs via @cursor/sdk. Requires CURSOR_API_KEY.
`;

type Args = {
  command: string;
  cwd: string;
  model: string;
  streamJson: boolean;
  outputFormatJson: boolean;
  agentId?: string;
  interactive: boolean;
  appendSystemPrompt?: string;
  help: boolean;
};

function parseArgs(argv: string[]): Args {
  const out: Args = {
    command: "",
    cwd: process.cwd(),
    model: "composer-2.5",
    streamJson: false,
    outputFormatJson: false,
    interactive: false,
    help: false,
  };
  let i = 0;
  if (argv[i] === "--help" || argv[i] === "-h") {
    out.help = true;
    return out;
  }
  if (argv[i]) {
    out.command = argv[i++];
  }
  while (i < argv.length) {
    const flag = argv[i++];
    switch (flag) {
      case "--cwd":
        out.cwd = argv[i++] ?? out.cwd;
        break;
      case "--model":
      case "-m":
        out.model = argv[i++] ?? out.model;
        break;
      case "--stream-json":
        out.streamJson = true;
        break;
      case "--output-format":
        if (argv[i] === "stream-json") {
          i++;
          out.streamJson = true;
        } else if (argv[i] === "json") {
          i++;
          out.outputFormatJson = true;
        }
        break;
      case "--agent-id":
        out.agentId = argv[i++];
        break;
      case "--interactive":
        out.interactive = true;
        break;
      case "--append-system-prompt":
        out.appendSystemPrompt = argv[i++];
        break;
      case "--help":
      case "-h":
        out.help = true;
        break;
      default:
        process.stderr.write(`kilroy-cursor-agent: unknown argument: ${flag}\n`);
        break;
    }
  }
  return out;
}

/** Map legacy CLI model IDs to Cursor SDK model IDs. */
export function toCursorModelId(raw: string): string {
  const id = raw.trim();
  if (!id) {
    return "composer-2.5";
  }
  const lower = id.toLowerCase();
  if (
    lower.startsWith("composer-") ||
    lower.startsWith("gpt-") ||
    lower.startsWith("claude-opus-4-") ||
    lower.startsWith("o3") ||
    lower.startsWith("o4")
  ) {
    return id;
  }
  if (lower.includes("haiku") || lower.includes("flash") || lower.includes("mini")) {
    return "composer-2-fast";
  }
  if (lower.includes("opus") || lower.includes("sonnet") || lower.includes("pro")) {
    return "composer-2.5";
  }
  if (lower.includes("codex") || lower.includes("gpt-5")) {
    return "composer-2.5";
  }
  return "composer-2.5";
}

function emitStreamLine(payload: Record<string, unknown>): void {
  process.stdout.write(JSON.stringify(payload) + "\n");
}

function emitAssistantText(text: string, model: string): void {
  if (!text.trim()) {
    return;
  }
  emitStreamLine({
    type: "assistant",
    message: {
      role: "assistant",
      model,
      content: [{ type: "text", text }],
    },
  });
}

function emitToolUse(
  callId: string,
  name: string,
  input: unknown,
  model: string,
): void {
  emitStreamLine({
    type: "assistant",
    message: {
      role: "assistant",
      model,
      content: [
        {
          type: "tool_use",
          id: callId,
          name,
          input: input ?? {},
        },
      ],
    },
  });
}

function emitToolResult(callId: string, content: string, isError: boolean): void {
  emitStreamLine({
    type: "user",
    message: {
      role: "user",
      content: [
        {
          type: "tool_result",
          tool_use_id: callId,
          content,
          is_error: isError,
        },
      ],
    },
  });
}

async function readStdinPrompt(): Promise<string> {
  const chunks: Buffer[] = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return Buffer.concat(chunks).toString("utf8");
}

async function runHeadless(args: Args, prompt: string): Promise<number> {
  const apiKey = process.env.CURSOR_API_KEY?.trim();
  if (!apiKey) {
    process.stderr.write("kilroy-cursor-agent: CURSOR_API_KEY is required\n");
    return 2;
  }

  const cwd = path.resolve(args.cwd);
  if (!fs.existsSync(cwd)) {
    process.stderr.write(`kilroy-cursor-agent: cwd does not exist: ${cwd}\n`);
    return 2;
  }

  const modelId = toCursorModelId(args.model);
  let fullPrompt = prompt;
  if (args.appendSystemPrompt?.trim()) {
    fullPrompt = `${args.appendSystemPrompt.trim()}\n\n${prompt}`;
  }

  let agent;
  try {
    agent =
      args.agentId != null && args.agentId !== ""
        ? await Agent.resume(args.agentId, { apiKey })
        : await Agent.create({
            apiKey,
            model: { id: modelId },
            local: { cwd },
          });
  } catch (err) {
    const message = err instanceof Error ? err.message : String(err);
    process.stderr.write(`kilroy-cursor-agent: agent startup failed: ${message}\n`);
    if (args.streamJson) {
      emitStreamLine({ type: "error", message });
    } else if (args.outputFormatJson) {
      process.stdout.write(
        JSON.stringify({ result: message, is_error: true }) + "\n",
      );
    }
    return 1;
  }

  let assistantTextEmitted = false;
  const emittedToolStarts = new Set<string>();

  try {
    const run = await agent.send(fullPrompt);

    for await (const event of run.stream()) {
      switch (event.type) {
        case "assistant": {
          const blocks = event.message?.content ?? [];
          const textParts: string[] = [];
          for (const block of blocks) {
            if (block.type === "text" && block.text) {
              textParts.push(block.text);
              if (args.interactive) {
                process.stdout.write(block.text);
              }
            }
          }
          const text = textParts.join("\n");
          if (args.streamJson && text.trim()) {
            emitAssistantText(text, modelId);
            assistantTextEmitted = true;
          } else if (!args.streamJson && !args.outputFormatJson && text) {
            process.stdout.write(text);
            if (!text.endsWith("\n")) {
              process.stdout.write("\n");
            }
          }
          break;
        }
        case "tool_call": {
          if (!args.streamJson) {
            break;
          }
          if (event.status === "running") {
            if (emittedToolStarts.has(event.call_id)) {
              break;
            }
            emittedToolStarts.add(event.call_id);
            emitToolUse(event.call_id, event.name, event.args ?? {}, modelId);
          } else if (event.status === "completed" || event.status === "error") {
            const resultText =
              event.result != null
                ? typeof event.result === "string"
                  ? event.result
                  : JSON.stringify(event.result)
                : "";
            emitToolResult(
              event.call_id,
              resultText,
              event.status === "error",
            );
          }
          break;
        }
        case "thinking":
          if (args.interactive && event.text) {
            process.stderr.write(event.text);
          }
          break;
        default:
          break;
      }
    }

    const result = await run.wait();
    if (result.status === "error") {
      const message = result.result ?? "unknown error";
      process.stderr.write(`kilroy-cursor-agent: run failed: ${message}\n`);
      if (args.streamJson) {
        emitStreamLine({ type: "error", message });
      } else if (args.outputFormatJson) {
        process.stdout.write(
          JSON.stringify({ result: message, is_error: true }) + "\n",
        );
      }
      return 1;
    }

    const finalText = result.result?.trim() ?? "";
    if (args.outputFormatJson) {
      process.stdout.write(
        JSON.stringify({ result: finalText, is_error: false }) + "\n",
      );
    } else if (args.streamJson) {
      if (finalText && !assistantTextEmitted) {
        emitAssistantText(finalText, modelId);
      }
      if (finalText) {
        emitStreamLine({ type: "result", result: finalText });
      }
    } else if (!args.interactive && finalText) {
      process.stdout.write(finalText);
      if (!finalText.endsWith("\n")) {
        process.stdout.write("\n");
      }
    }
    return 0;
  } finally {
    await agent[Symbol.asyncDispose]();
  }
}

async function main(): Promise<void> {
  const args = parseArgs(process.argv.slice(2));
  if (args.help || args.command === "--help") {
    process.stdout.write(HELP);
    process.exit(0);
  }
  if (args.command !== "run") {
    process.stderr.write(HELP);
    process.exit(2);
  }

  const promptFromStdin = await readStdinPrompt();
  const code = await runHeadless(args, promptFromStdin);
  process.exit(code);
}

main().catch((err) => {
  const message = err instanceof Error ? err.message : String(err);
  process.stderr.write(`kilroy-cursor-agent: ${message}\n`);
  process.exit(1);
});
