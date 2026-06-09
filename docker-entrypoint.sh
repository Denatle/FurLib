#!/bin/sh
# Overwrite Docker's embedded DNS proxy with real nameservers.
# Needed on Windows/WSL2 where 127.0.0.11 often fails to forward.
printf 'nameserver 8.8.8.8\nnameserver 1.1.1.1\n' > /etc/resolv.conf
exec "$@"
