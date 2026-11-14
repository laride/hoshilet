#!/usr/bin/env node

// Generates cli/npm/README.md or cli/python/README.md by combining
// a package-specific header (badges + install) with the shared
// docs/en/cli-usage.md content (starting from "## Quick Start").

import { readFileSync, writeFileSync } from "node:fs";
import { resolve, dirname } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const root = resolve(__dirname, "..");

const target = process.argv[2];
if (target !== "npm" && target !== "python") {
  console.error("Usage: gen-cli-readme.mjs <npm|python>");
  process.exit(1);
}

const shared = readFileSync(resolve(root, "docs/en/cli-usage.md"), "utf8");

// Extract everything from "## Quick Start" onward.
const startMarker = "## Quick Start";
const idx = shared.indexOf(startMarker);
if (idx === -1) {
  console.error('Could not find "## Quick Start" in docs/en/cli-usage.md');
  process.exit(1);
}
const sharedBody = shared.slice(idx);

const headers = {
  npm: `# hoshilet

[![NPM Version](https://img.shields.io/npm/v/hoshilet)](https://www.npmjs.com/package/hoshilet)
[![GitHub Repo](https://img.shields.io/badge/github-repo-blue?logo=github)](https://github.com/laride/hoshilet)

Deploy static sites to a [hoshilet](https://github.com/laride/hoshilet) instance.

Also available as a [Python package](https://pypi.org/project/hoshilet/).

## Install

\`\`\`sh
npm install -g hoshilet
\`\`\`

Requires Node.js 22 or later.

`,
  python: `# hoshilet

[![PyPI Version](https://img.shields.io/pypi/v/hoshilet)](https://pypi.org/project/hoshilet/)
[![GitHub Repo](https://img.shields.io/badge/github-repo-blue?logo=github)](https://github.com/laride/hoshilet)

Deploy static sites to a [hoshilet](https://github.com/laride/hoshilet) instance.

Also available as an [npm package](https://www.npmjs.com/package/hoshilet).

## Install

\`\`\`sh
pip install hoshilet
# or
uv tool install hoshilet
\`\`\`

Requires Python 3.10\u20133.15.

`,
};

const readme = headers[target] + sharedBody;

const outPath =
  target === "npm"
    ? resolve(root, "cli/npm/README.md")
    : resolve(root, "cli/python/README.md");

writeFileSync(outPath, readme);
console.log(`Generated ${outPath}`);
