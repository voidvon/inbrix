# Repository Agent Rules

## shadcn/ui components

- `frontend/src/components/ui/` is the dedicated directory for official shadcn/ui component source code.
- Treat every file in `frontend/src/components/ui/` as generated, read-only vendor code. Never manually edit, reformat, extend, patch, or add project-specific behavior or styling to these files.
- Add or update shadcn/ui components only with the official shadcn CLI. The resulting official generated source must remain unchanged.
- Put application-specific wrappers, composed components, and custom UI outside `frontend/src/components/ui/` (for example under `frontend/src/components/app/`).
- Configure component behavior from the consumer through the official component API and props. If the official API is insufficient, create a wrapper outside the official directory; do not modify the shadcn/ui source.
- Before completing a frontend change, verify that no file under `frontend/src/components/ui/` differs from the exact output produced by the official shadcn CLI for the installed style and version.
