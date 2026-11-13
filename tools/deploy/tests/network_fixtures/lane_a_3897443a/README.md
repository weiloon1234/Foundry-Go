# Checkpoint-2 consumer fixtures

`admission.py`, `services.py` and `feature_network.py` are unedited copies of
`tools/deploy/lib/<name>` at lane A commit `3897443a`. They are test inputs,
not runtime implementations. Recreate each with:

```sh
git show 3897443a:tools/deploy/lib/admission.py
git show 3897443a:tools/deploy/lib/services.py
git show 3897443a:tools/deploy/lib/feature_network.py
```

`network_seam_fixture.py` loads these owners only inside a test context. It builds
the launch, transition, selection and role-to-unit records used by checkpoint-2
`feature_lifecycle.py`. `contracts.py` is identical on that checkpoint and this
lane, so both use the live frozen contracts. No lane A file is installed or edited.

The unit tests exercise real protected files, admission receipts and permits while
substituting kernel observations. The opt-in container closure test substitutes
only a protected fixture registry reader: it does not provision an application or
exercise lane A's schema-3 registry integration. Systemd, cgroups, UFW, nginx and
socket observations in that scenario are real.
