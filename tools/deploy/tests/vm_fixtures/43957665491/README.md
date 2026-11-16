# VM source receipts

Public, already-redacted artifacts from CI run `43957665491`, revision `6a1fe4ea`,
Ubuntu 24.04. Each JSON file preserves values from that topology's VM A:

- `status`: `*-a-status-source.log`; retained instance identity/profile/topology,
  allocations, services, current release, backup state and deployment backup linkage.
- `reports`: `*-a-report-source.log` and `*-a-report-root-source.log`; host identity,
  completeness and problems.
- `units`: `*-a-role-source-*.log`, unchanged systemctl output.
- `deploy`: top-level documents containing `version` from `*-a-deploy-source.log`.

Unused status history and report project details are omitted. No private material
is included. Standard's first backup is a pre-migration `recovery` bundle, linked
through `last_attempt`; feature's first backup is `full`, linked through
`backup.initial_operation_id`. Keys alone do not claim PostgreSQL coverage.
