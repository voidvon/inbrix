// eslint.config.js — flat config for inbrix's static client JS.
//
// Scope: the service worker and any remaining standalone browser scripts.
//
// Type-aware: parserOptions.projectService normally auto-discovers the
// nearest tsconfig.json per file, but there is only one tsconfig.json in
// this repo (root, for assets/js/ — DOM lib) — tsconfig.sw.json (Service
// Worker lib, for assets/sw.js) is deliberately NOT named tsconfig.json (two
// files can't share that name for directory-based discovery) and lives
// outside assets/ (which //go:embed all:assets compiles into the release
// binary — see assets_embed_test.go and tsconfig.sw.json's own comment).
// assets/sw.js is therefore routed to it explicitly via
// defaultProject/allowDefaultProject rather than relying on discovery.
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    ignores: [
      'node_modules/**',
      'scripts/**',
    ],
  },
  {
    // Not `projectService: { defaultProject, allowDefaultProject }` here:
    // that combination (routing one out-of-project file to a non-"tsconfig.
    // json"-named project) reproducibly corrupted allowDefaultProject during
    // config merging in this eslint 10 + typescript-eslint 8.66 pairing
    // (verified with `eslint --print-config`: the array came out as an
    // indexed object, and linting then failed with "was not found by the
    // project service"). `parserOptions.project` — the older, still fully
    // type-aware, still fully supported form — points at tsconfig.sw.json
    // directly with no auto-discovery involved, and has none of that issue.
    files: ['assets/sw.js'],
    extends: [...tseslint.configs.recommendedTypeChecked],
    languageOptions: {
      ecmaVersion: 2020,
      sourceType: 'script',
      parserOptions: {
        project: ['./tsconfig.sw.json'],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      'no-undef': 'off',
    },
  },
  {
    files: ['frontend/**/*.{ts,tsx}'],
    extends: [...tseslint.configs.recommendedTypeChecked],
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: 'module',
      parserOptions: {
        project: ['./frontend/tsconfig.json'],
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      'no-undef': 'off',
    },
  },
);
