# Agent Instructions

## Operational Loop

Before implementing any new code or fixing bugs:

1. Read `SPEC.md` to understand the current state and requirements.
2. If a requested feature or change deviates from or expands `SPEC.md`, update `SPEC.md` first to reflect the new design and implementation plan.
3. Propose the `SPEC.md` modifications to the user.
4. Once approved, write the code exactly as specified.
5. Update the status checklist in `SPEC.md` upon completion.

## Technical Constraints

1. **Architecture:** Maintain a flat directory structure. Do not create sub-directories for Go code.
2. **Dependencies:** Rely strictly on the Go standard library and third-party modules that are already used in the project. Do not introduce external modules without explicit permission.
3. **Style:** Write idiomatic, minimal Go. Prefer exact, predictable behavior over heavy abstractions.
4. **Verification:** Every Go file except `main.go` must come with a `*_test.go` file achieving. Tests must be kept as simple as possible such that they are easily seen to test the right thing.
