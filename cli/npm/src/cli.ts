#!/usr/bin/env node
import { main } from './hoshilet.ts';

main()
  .then((code) => {
    process.exitCode = code;
  })
  .catch((error: unknown) => {
    const message = error instanceof Error ? error.message : String(error);
    console.error(process.argv.includes('--json') ? JSON.stringify({ error: { message } }) : message);
    process.exitCode = 1;
  });
