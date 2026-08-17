# Bitwave web application parity

Status: living implementation inventory

This document maps customer-facing Bitwave web application capabilities to the
organization-mode CLI. The source of truth is the active UI routes and the
backend requests made by their components, not the text of navigation labels.

## Parity contract

A capability is CLI-complete when it provides:

- the same supported backend operation as the UI;
- explicit organization and resource identifiers;
- machine-readable input and output;
- non-interactive execution;
- dry-run and explicit confirmation for material mutations;
- status/readback commands for asynchronous work;
- complete pagination or an explicit bounded result;
- backend errors without hiding route, status, or response details.

Interactive provider authorization, hardware signing, file selection, and
visual dashboards may still require a browser or device. The CLI should expose
the underlying data, initiation request, status, and downloadable artifacts
where the backend supports them.

## Capability matrix

| Product area | UI capabilities | Current CLI coverage | State |
|---|---|---|---|
| Authentication | Sign in/out, account context | OAuth login, cached refreshable org sessions | Partial |
| Organizations | Select, create, inspect settings | List, use, current, create, clear | Partial |
| Users and invitations | Invite, list, update, remove users | None | Missing |
| Roles and permissions | Organization roles, wallet roles | None | Missing |
| API access | API credentials and access settings | Token consumption only | Missing |
| Audit log | Organization audit events | None | Missing |
| Subsidiaries | List and manage subsidiaries | Discovery for report filters only | Partial |
| Wallets and sources | List, add, update, delete, roles, sync configuration | List, network discovery, add/batch add, Babel rollup get/set | Partial |
| Portfolio and dashboards | Portfolio, accounting, NFT and transaction summaries | Report/filter data only | Partial |
| Transactions | Search, drilldown, create, ignore, categorize, combine/split, reconcile and related actions | Search/get, simple/trade/transfer create, ignore/unignore, categorize/bulk categorize | Partial |
| Matching and reconciliation | Transaction matching and reconciliation workflows | None | Missing |
| Rules | List, create, edit, enable/disable, validate and execute | List/get/create/update/enable/disable/delete/validate/run/bulk-run | Supported |
| Categories | List/export/create, enable/disable, disable all | List, create/import, enable/disable and disable all; CSV export missing | Partial |
| Contacts | List/export/create, enable/disable, defaults, connection and address mappings | List, create, complete-input update, enable/disable and disable all; CSV export missing | Partial |
| Accounting connections | List, configure, synchronize and manage providers | List and create manual connection | Partial |
| Imports and data load | Transaction import, data import workflows and reports | None | Missing |
| Data platform | Explore, sources, feeds, executions, schemas, transforms, rollups and reconciliation | Wallet Babel rollup configuration only | Partial |
| Pricing | History, rules, rate tables, contexts and routes | None | Missing |
| Inventory views | List/create/edit/delete, update runs, scenarios and management | List/create/delete, trigger update, list update results | Partial |
| Gain/loss | Scenario runner and reports | Actions report only | Partial |
| Reports | Balance, transaction export, journal, expanded, rolled-up, ledger, balance check and export history | Balance, Transaction Export and Actions | Partial |
| Period close | Close configuration and period workflow | None | Missing |
| External cost basis | Import and manage external basis | None | Missing |
| Wrapping and tax strategy | Configure product treatments | None | Missing |
| Token filtering | Organization token filtering | None | Missing |
| Invoices, bills and customers | AR/AP records and categorization support | Invoice-aware categorization payloads only | Partial |
| Crypto bills and payments | Bills and bulk payment workflows | None | Missing |
| Marketplace and onramp | Discover/install integrations and onboarding flows | None | Missing |
| SSO and SCIM | Enterprise identity configuration | None | Missing |
| System jobs | Inspect background jobs | Rule/inventory-specific status only | Partial |

## Delivery order

1. Complete foundational CRUD and exports for categories, contacts, wallets,
   subsidiaries, users, and accounting resources.
2. Complete transaction actions, matching, reconciliation, and import workflows.
3. Complete pricing and inventory management/scenario operations.
4. Expose every named report and export-history operation.
5. Add period-close state and system-job observability.
6. Add AR/AP, data-platform, enterprise administration, and integration flows.

Each slice should be derived from current UI request contracts and shipped with
contract tests. A missing backend operation is recorded as blocked rather than
implemented as a misleading client-side approximation.
