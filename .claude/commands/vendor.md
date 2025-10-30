---
description: Vendor openshift/api at a specific commit SHA
---

You are helping vendor the openshift/api repository at a specific commit SHA.

# Usage

The user can invoke this command with a commit SHA:
- `/vendor <SHA>` - Vendor openshift/api at the specified commit SHA
- `/vendor` - If no SHA provided, ask the user for it

# Task

Follow these steps to vendor openshift/api at the specified commit:

1. **Extract the SHA from the command arguments**:
   - The SHA should be provided as the first argument after /vendor
   - If no SHA is provided, ask the user: "Please provide the commit SHA from openshift/api to vendor (e.g., /vendor abc123...)"
   - The SHA can be either full (40 characters) or short (7+ characters)

2. **Show current version**:
   - Run: `grep 'github.com/openshift/api' go.mod` to show the current version
   - Display it to the user so they know what will change

3. **Update the openshift/api dependency to the specified SHA**:
   - Run: `go get github.com/openshift/api@<SHA>`
   - If this fails (invalid SHA), inform the user and ask for a valid SHA

4. **Clean mod cache, tidy and update vendor directory**:
   - Run: `go clean -modcache; go mod tidy && go mod vendor`
   - This ensures a clean state by clearing the module cache, tidying go.mod, and updating the vendor directory

5. **Verify the changes**:
   - Run: `go mod verify`

6. **Verify Functionality**:
   - Run `make test-unit` to verify if the vendored code did not break any present functionality
   - If the tests fail, analyze the error carefully
   - Fix each error by editing the relevant source files
   - Re-run only the failing tests by setting the `TESTABLE` envar, which represents the go package the failing test is from
   - Continue until all tests pass successfully (max 3 fix attempts)
   - If tests still fail after 3 fix attempts, report the remaining failures to the user and ask how to proceed
   - Once fixed birefly summarize the natuze or the fix in a `<CASUE>` variable which will be used when commitign the changes.

7. **Commit changes**:
   1. commit changes `go.mod`, `go.sum` and `/vendor` as a separate commit in format `Bump API: <SHA>`. Prefix the commit with the `<JIRA>: ` if available
   2. commit changes other change as a separate commit in format `Bump API: <CAUSE>`. Prefix the commit with the `<JIRA>: ` if available

8. **Show summary**:
   - Run: `grep 'github.com/openshift/api' go.mod` to show the new version
   - Display what changed (old version → new version)
   - Confirm successful vendoring

# Important Notes

- The console-operator uses openshift/api as a dependency
- Vendoring ensures all dependencies are stored in the vendor/ directory
- The SHA must be a valid commit from github.com/openshift/api
- After vendoring, the user should review and commit the changes to git
- All vendor/ changes should be included in the commit

# Output Format

Provide clear, concise updates at each step so the user knows what's happening.
