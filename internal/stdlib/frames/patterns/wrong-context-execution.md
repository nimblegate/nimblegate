---
id: wrong-context-execution
description: Operating on resource A while in context for resource B - commit to wrong repo, deploy to wrong cluster, bill to wrong project.
anticipated-siblings: []
---

# Pattern: wrong-context-execution

The command is correct. The arguments are correct. The execution context is wrong: cwd is in repo A but the user thought they were in repo B. The kubectl context is staging but the user thought it was set to prod (and the command they ran was safe to run in prod). The implicit context - whatever tool maps to "current state" without saying it aloud - silently rerouted the action.

The structural defense: make the context part of every action. Compare cwd against expected repo before commit. Echo the active cluster before kubectl. Print the active gcloud project before deploy. Verbose feels redundant 99% of the time; the 1% pays for all of it.
