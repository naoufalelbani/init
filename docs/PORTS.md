# Port allocation

Port records are project-local at `.ai-init/ports.json`, which is ignored by Git. `ai-init wt new <task> --port <service>` allocates ports only when explicitly requested. Allocations are keyed by task and service, stable for repeated requests, and removed when the task is integrated or safely cleaned up. `ai-init ports list|release` manages records directly.

A cross-process directory lock serializes record changes. The operating system selects an available loopback TCP port while that lock is held; ai-init closes the temporary listener before the application binds. This avoids duplicate ai-init records but cannot eliminate the operating-system race where another process binds the selected port first. The application must handle bind errors. ai-init does not copy `.env` files or mutate databases/shared services.
