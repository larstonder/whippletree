# adapter-sdk

Declare an agent tool's lifecycle requirements once; compile them onto multiple
AI coding harnesses at the best verifiable fidelity tier. Class-1 spine:
Claude Code + Codex CLI. Spec: harness-adapter.architecture.md (presentations repo).

- `adapter-sdk build <bundle-dir>`: compile per-target variants
- `adapter-sdk preflight <bundle-dir> --target <name>`: probe + tier report
- `adapter-hook run <event> --target <name>`: hook dispatcher (invoked by harnesses, not humans)
