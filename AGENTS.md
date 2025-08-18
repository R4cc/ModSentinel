# ModSentinel Contributor Guidelines

## Scope
These instructions apply to the entire repository unless a nested `AGENTS.md` overrides them.

## Project Overview
- **Backend:** Go 1.24.3 application in the repository root.
- **Frontend:** React interface in `frontend/` built with Vite and Node.js 20.

## Search & Navigation
- Use [`rg`](https://github.com/BurntSushi/ripgrep) for all code searches.

## Coding Standards
### Go
- Format files with `gofmt -w` before committing.
- Organize imports with standard library packages first, then external modules.
- Use idiomatic Go naming conventions (`err` for errors, `CamelCase` for exported identifiers, `snake_case` for JSON tags).

### JavaScript/JSX
- Use ES modules and React functional components.
- Prefer single quotes and terminate statements with semicolons.
- Keep indentation at two spaces.

## Dependency Management
- Backend uses Go modules (`go.mod`, `go.sum`). Run `go mod tidy` when adding or removing imports.
- Frontend uses npm with a `package-lock.json`. Install packages with `npm ci` and commit updated lockfiles.

## Testing & Validation
Run the following checks from the repository root after making changes:
- `go test ./...` – ensures Go code compiles and tests (if any) pass.
- If backend code was modified, also run `go vet ./...` and `go build`.
- If frontend code was touched, run inside `frontend/`:
  - `npm test` (when tests exist) or `npm run build` to ensure the project builds.

## Development Tips
- Build the backend with `go build` and run the resulting `./modsentinel` binary (listens on `:8080`).
- Build or preview the frontend with `npm run dev` or `npm run build` in `frontend/`.

## Commit & PR Guidelines
- Use concise, imperative commit messages ("Add feature" not "Adding feature").
- Pull request descriptions should include:
  - **Summary** – brief explanation of the change.
  - **Testing** – commands run and their results.

## Documentation
- Update relevant documentation (e.g., `README.md`) when behavior or commands change.

## Security Note
ModSentinel is intended for local use only. Do not expose the application directly to the public internet.

