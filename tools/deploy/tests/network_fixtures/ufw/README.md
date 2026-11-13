# Ubuntu UFW reference fixtures

The six `*.rules` files and two `ufw-init*` files are unmodified files from the
Ubuntu package [ufw 0.36.2-6](https://archive.ubuntu.com/ubuntu/pool/main/u/ufw/ufw_0.36.2-6_all.deb).
The rules originate in `/usr/share/ufw/iptables/`; the installed reload machinery
originates in `/usr/lib/ufw/`. They are distributed under GPL-3.0; see `COPYING`.
Copyright notices in the original scripts are preserved.

The same files were compared with the Ubuntu packages
[0.36.2-9build1](https://archive.ubuntu.com/ubuntu/pool/main/u/ufw/ufw_0.36.2-9build1_all.deb)
and [0.36.2-10](https://archive.ubuntu.com/ubuntu/pool/main/u/ufw/ufw_0.36.2-10_all.deb).
Their reload script bytes are identical. The implementation accepts these reviewed
script digests, not arbitrary executable reload code or shell-valued settings:

| File | SHA-256 |
| --- | --- |
| ufw-init | c98f19dd0a68e94f729acdcba34fe48244dbfacdfa8943f257215d2ae351f80a |
| ufw-init-functions | 5865fbffc0c644d97e5fc6906c648ce6e4f23710ea0ff57c573ed680999d2cc4 |

`network_firewall_fixture.py` adds a test-owned SSH allow and Foundry integration
to copies of the real files. It models the kernel graph produced by the upstream
reload procedure, including primary chains, policy helpers, logging helpers and
the appended user-chain jumps. It uses `ufw-*` for IPv4 and `ufw6-*` for IPv6.

Both backend command adapters use that graph. The nft JSON adapter models native
match/verdict and compatibility expressions with exact per-chain rule accounting;
it is not a captured kernel dump. Tests inject foreign tables, incompatible
expressions, missing rules, changed hooks and decoder warnings. No fixture executes
UFW, iptables, nftables, nginx, systemd, DNS or a provider API. The opt-in M6 container
scenarios remain the owner of real-kernel and real-nginx acceptance.
