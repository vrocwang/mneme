# Extension manifest.json reference

Every extension ships a `manifest.json` in its directory. The host discovers
extensions at startup and auto-builds them if a build command is present.

## Fields

| Field | Required | Description |
|-------|----------|-------------|
| `name` | yes | Unique extension name |
| `version` | yes | Semver version |
| `category` | yes | `skills`, `channels`, `integrations`, `agents`, `desktop`, etc. |
| `binary` | yes | Executable path or interpreter command (see below) |
| `build` | no | Shell command to compile before loading. Run from extension dir. |
| `description` | no | Human-readable description |
| `tools` | no | Tool names provided by this extension |
| `author` | no | Author name |
| `config_schema` | no | JSON Schema for extension settings |

## Binary field

The `binary` field can be:

1. **Native binary name** — relative to the extension directory:
   ```json
   { "binary": "skill-runtime" }
   ```

2. **Interpreter invocation** — for scripts:
   ```json
   { "binary": "python3 my_skill.py" }
   { "binary": "node dist/index.js" }
   ```

3. **Script file** — if shebang is present, interpreter is auto-detected:
   ```json
   { "binary": "my_skill.py" }
   ```

## Build field

When present, the host runs this command before loading. The command runs in
the extension's directory. Only runs if the binary doesn't already exist.

## Examples

### Go extension
```json
{
  "name": "my-go-ext",
  "version": "1.0.0",
  "category": "skills",
  "binary": "my-ext",
  "build": "go build -ldflags=\"-s -w\" -o my-ext .",
  "tools": ["my_tool"]
}
```

### Rust extension
```json
{
  "name": "my-rust-ext",
  "version": "1.0.0",
  "category": "skills",
  "binary": "target/release/my-ext",
  "build": "cargo build --release",
  "tools": ["rust_tool"]
}
```

### C/C++ extension (Makefile)
```json
{
  "name": "my-c-ext",
  "version": "1.0.0",
  "category": "integrations",
  "binary": "build/my-ext",
  "build": "make",
  "tools": ["c_tool"]
}
```

### Python extension (no build needed)
```json
{
  "name": "my-python-skill",
  "version": "1.0.0",
  "category": "skills",
  "binary": "python3 skill.py",
  "tools": ["python_tool"]
}
```

### Node.js extension (build needed)
```json
{
  "name": "my-node-ext",
  "version": "1.0.0",
  "category": "skills",
  "binary": "node dist/index.js",
  "build": "npm install && npm run build",
  "tools": ["node_tool"]
}
```
