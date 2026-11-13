# Captured Ubuntu UFW layouts

These 75 files are byte-for-byte copies of the operator's `ufw-real/` captures
supplied for the second lane C review on 2026-11-14. Capture timestamps appear in
`iptables-save.txt` and `ip6tables-save.txt`. No rule, comment or output was edited.
`SHA256SUMS` records the copied bytes. UFW's source files retain their original
notices; see the package [license](../ufw/COPYING).

| Directory | Backend | Evidence |
| --- | --- | --- |
| `24.04/` | nf_tables, iptables 1.8.10 | Fresh and enabled `/etc/ufw`, `/etc/default/ufw`, modes, live rules, status and enable output |
| `26.04/` | nf_tables, iptables 1.8.11 | Fresh and enabled `/etc/ufw`, `/etc/default/ufw`, modes, live rules, status and enable output |
| `24.04-legacy/` | legacy, iptables 1.8.10 | Enabled `/etc/ufw`, live rules, version and enable output |

Enabled captures follow `ufw default deny incoming`, allows for TCP 22, 80 and
443, logging low, then `ufw --force enable`. Both installed init hooks have mode
0640. Both generated user rule files insert an INVALID/RETURN rule at the head
of the family-specific logging-deny chain.

`test_network_real.py` loads the enabled files unchanged into temporary roots,
reinstating the recorded file modes. Legacy's missing settings/status/mode files
are explicitly supplied from the same Ubuntu 24.04 capture. The validated reload
scripts come from the existing unmodified [package fixtures](../ufw/README.md).
Tests replay the captured `iptables-save` and `ip6tables-save` output, check the
reconstructed helper graph against it, and prove persistent projection preserves
every foreign byte. Negative variants change only temporary copies.

The supplied `nft-ruleset.txt` is **text**, whereas production calls `nft -j list
ruleset`. Tests compare its table/chain rule counts with the captured compatibility
views. Static observation replay uses the existing structural JSON adapter at
that command boundary; it does not claim the JSON bytes came from a live kernel.
The opt-in `UFWObservationAcceptance` in `acceptance_network.py` exercises the
actual JSON command and both live compatibility views without mocks. See the exact
[container command](../../NETWORK_HANDOFF.md#live-ubuntu-ufw-observation).
