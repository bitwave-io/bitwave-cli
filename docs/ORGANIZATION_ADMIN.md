# Organization administration

`bitwave org admin` exposes the backend operations used by Bitwave's
Administration navigation. It is intended for organization administrators and
automation. It does not bypass Bitwave scopes, roles, feature flags, or backend
validation.

## Discover the surface

```bash
bitwave org admin capabilities
bitwave org admin capabilities --json
bitwave org admin capabilities --area connections --json
bitwave org admin connections --help
```

The catalog currently covers 100+ operations across:

- organization settings and subsidiaries;
- accounting setup and billing;
- accounting connections, including the complete NetSuite settings surface;
- system jobs and administrative wallet operations;
- users, invitations, roles, SSO, SCIM, and API credentials;
- audit configuration, custom labels, SFTP, and rolled-up JE configuration.

Feature-gated operations are always discoverable. The selected organization's
backend remains the authority on whether a feature and the caller's required
scope are enabled.

## Input and output

Commands use JSON objects because the Admin API contracts include provider-
specific and evolving settings:

```bash
bitwave org admin organization update \
  --data '{"displayTimezone":"America/New_York"}' \
  --dry-run

bitwave org admin organization update \
  --input organization-settings.json \
  --yes
```

`--input -` reads JSON from standard input. `--org` overrides the active
organization, `--query key=value` may be repeated for REST filters, and `--out`
writes the structured response envelope to a file.

All operations produce machine-readable JSON. Writes require `--yes`; every
write can be inspected first with `--dry-run`. Organization and SSO writes load
the current ETag automatically to prevent lost updates.

## NetSuite settings

NetSuite has first-class commands for connection creation, credentials,
account mappings, general settings, custom body/line fields, custom segments,
saved searches, custom records/reference mappings, metadata mappers, and
invoice/bill subsidiary routing:

```bash
bitwave org admin connections netsuite-custom-segments CONNECTION_ID \
  --input netsuite-settings.json \
  --dry-run
```

The backend update matches the UI contract and accepts the complete
`connectionSpecificFields` object. Read the current object with `connections
list`, modify only the intended nested collection, and send the complete object
so unrelated NetSuite settings are retained.

## System jobs

Named commands supply the same job and action identifiers as the Admin UI:

```bash
bitwave org admin system-jobs ignore \
  --data '{"walletId":"WALLET_ID","matchCriteria":{"direction":"Inbound"}}' \
  --dry-run

bitwave org admin system-jobs list --query limit=50
```

The generic `system-jobs run` command remains available for newly deployed job
types before a dedicated command is released.

## Safety and credentials

- Authentication comes from the normal Bitwave CLI session or `--token`.
- Secrets supplied in JSON are sent only to fixed Bitwave service hosts.
- Relative endpoint construction prevents forwarding a Bitwave token to an
  arbitrary host.
- Dry-run output reports request size and decoded JSON. Treat previews that
  contain credentials as sensitive.
