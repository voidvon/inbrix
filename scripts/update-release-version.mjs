#!/usr/bin/env node

import { readFile, writeFile } from "node:fs/promises";
import { resolve } from "node:path";
import process from "node:process";

const [oldVersion, newVersion] = process.argv.slice(2);
const semver = /^\d+\.\d+\.\d+$/;

if (!semver.test(oldVersion || "") || !semver.test(newVersion || "")) {
  console.error("Usage: update-release-version.mjs OLD_VERSION NEW_VERSION");
  process.exit(2);
}

const root = resolve(import.meta.dirname, "..");
const replacements = [
  ["README.md", [[oldVersion, newVersion]]],
  ["README_CN.md", [[oldVersion, newVersion]]],
  ["site/docs.html", [[oldVersion, newVersion]]],
  ["site/index.html", [
    [oldVersion, newVersion],
    [`No. ${oldVersion.split(".").slice(0, 2).join(".")}`, `No. ${newVersion.split(".").slice(0, 2).join(".")}`],
  ]],
];

const declaredVersion = (await readFile(resolve(root, "VERSION"), "utf8")).trim();
if (declaredVersion !== oldVersion) {
  throw new Error(`VERSION contains ${JSON.stringify(declaredVersion)}, expected ${JSON.stringify(oldVersion)}`);
}
await writeFile(resolve(root, "VERSION"), `${newVersion}\n`);

for (const [relativePath, rules] of replacements) {
  const path = resolve(root, relativePath);
  let content = await readFile(path, "utf8");
  for (const [from, to] of rules) {
    if (from === to) continue;
    if (!content.includes(from)) throw new Error(`${relativePath} does not contain expected version marker ${JSON.stringify(from)}`);
    content = content.replaceAll(from, to);
  }
  await writeFile(path, content);
}

console.log(`Updated release version markers: ${oldVersion} -> ${newVersion}`);
