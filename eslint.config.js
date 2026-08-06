// eslint.config.js — flat config for lilmail's static client JS.
//
// Scope: assets/js/*.js (browser scripts extracted from templates/layouts/
// main.html) and assets/sw.js (the Service Worker). Vendored bundles under
// assets/vendor/ (htmx, alpine) are excluded — they are third-party code we
// do not maintain and do not want flagged.
//
// Type-aware: parserOptions.projectService discovers the nearest tsconfig.json
// per file — tsconfig.json (DOM lib) for assets/js/, assets/tsconfig.json
// (Service Worker lib) for assets/sw.js — matching npm run typecheck.
import tseslint from 'typescript-eslint';

export default tseslint.config(
  {
    ignores: [
      'assets/vendor/**',
      'node_modules/**',
      'scripts/**',
    ],
  },
  {
    files: ['assets/js/**/*.js', 'assets/sw.js'],
    extends: [...tseslint.configs.recommendedTypeChecked],
    languageOptions: {
      ecmaVersion: 2020,
      sourceType: 'script',
      parserOptions: {
        projectService: true,
        tsconfigRootDir: import.meta.dirname,
      },
    },
    rules: {
      // TypeScript (via checkJs) already reports every undefined-reference
      // case this would catch, with far better accuracy for ambient/DOM/
      // Service-Worker globals; typescript-eslint's own docs recommend
      // disabling it for exactly this reason.
      'no-undef': 'off',
    },
  },
);
