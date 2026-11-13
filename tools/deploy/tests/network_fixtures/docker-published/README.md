# Docker published-port policy examples

These are authored examples in Docker's iptables bridge format, not operator
captures. The unedited Docker embedded-DNS captures live in
[ufw-real/docker-acceptance-host](../ufw-real/docker-acceptance-host/).

Both examples include the LOCAL-address PREROUTING/OUTPUT jumps, bridge RETURN,
published-port DNAT, and POSTROUTING MASQUERADE. `nonclaimed.rules` publishes TCP
8080; `claimed.rules` publishes TCP 8554, the claim in `test_network_nat.py`.
Tests replay both with legacy and nft backends and substitute IPv6 addresses in
memory for the IPv6 cases. Unsafe variants alter only temporary rule strings.
