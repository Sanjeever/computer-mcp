import { launch } from "./launcher.js";

try {
  launch();
} catch (error) {
  console.error(error instanceof Error ? error.message : String(error));
  process.exitCode = 1;
}
