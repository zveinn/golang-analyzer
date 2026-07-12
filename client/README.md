# analyze

Tiny CLI to trigger analysis in the code-analyzer server.

## Usage

```bash
./analyze <file> <line>
```

Example:

```bash
./analyze src/main.rs 42
```

This sends `src/main.rs:42` to the analyzer listening on TCP port 2222.

## From Helix

```toml
[keys.normal]
b = [
  ":sh",
  "/home/sveinn/code/code-analyzer/client/target/release/analyze %{buffer_name} %{cursor_line}"
]
```

You can also copy/symlink the binary to `~/.local/bin/analyze` (or anywhere in `$PATH`) to make the command shorter.

## Environment

- `ANALYZER_ADDR` — override the target address (default: `127.0.0.1:2222`)
