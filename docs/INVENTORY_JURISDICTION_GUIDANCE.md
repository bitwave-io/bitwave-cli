# Inventory Views and Jurisdiction Guidance

`bitwave inventory` lets an LLM list, create, and start calculations for
Bitwave inventory views. A jurisdiction profile is a reviewable starting
configuration and prompt set. It is not legal, tax, accounting, or financial
advice, and it must never be presented as a conclusion about a user.

## Required LLM flow

Before creating a view, distinguish among:

1. financial-statement books;
2. federal tax;
3. state and local tax;
4. management reporting; and
5. balance/reconciliation-only reporting.

Location alone does not answer that question. Ask for the entity type,
reporting framework, fiscal year, industry-specific guidance, filing
jurisdictions, asset scope, pricing policy, wallet/account mapping, lot method,
fee policy, and records supporting any specific-identification election. Ask a
qualified accountant to approve the setup. Guidance is advisory and never
blocks an explicit user override.

Run:

```bash
bitwave inventory guidance --jurisdiction US
bitwave inventory create --profile us-gaap --dry-run
bitwave inventory create --profile us-gaap --yes
bitwave inventory update "US GAAP - Fair Value" --yes
bitwave inventory delete "US GAAP - Fair Value" --dry-run
```

All writes require `--yes`; `--dry-run` returns the exact request without
changing the organization. Creation is idempotent by exact case-insensitive
view name.

## U.S. GAAP books profile

`us-gaap` is a starting view for a company that has confirmed it issues U.S.
GAAP financial statements:

- engine v2.9;
- FIFO operational lot selection, per wallet;
- GAAP fair-value valuation for in-scope crypto assets;
- trading fees capitalized under the selected Bitwave policy;
- valuation pricing explicitly inherited from the organization's configured
  default rather than left unspecified;
- NFTs excluded because they do not meet ASU 2023-08's fungibility scope
  criterion; and
- original acquisition dates preserved for wallet-level internal transfers.

Fee capitalization is a configurable accounting-policy decision, not a
universal jurisdiction rule; the accountant must confirm it. FIFO is not
represented as a FASB requirement. The accountant must determine
which assets meet every ASU 2023-08 scope criterion and separately analyze
NFTs, issued or related-party assets, assets carrying enforceable rights,
wrapped or receipt tokens, staking, DeFi, derivatives, and industry-specific
guidance. U.S. GAAP books do not determine tax treatment.

## U.S. federal tax profile

`us-federal-tax-fifo` is a separate starting view:

- FIFO;
- inventory mapped per wallet/account;
- historical cost rather than book fair-value remeasurement;
- trading transaction costs reflected in the inventory calculation; and
- original acquisition dates preserved for internal transfers.

For dispositions on or after January 1, 2025, the IRS wallet/account rules
make a universal multi-wallet basis pool inappropriate. Specific
identification requires timely identification and adequate substantiating
records; otherwise the CLI surfaces FIFO as the federal default. Transaction
costs for acquisitions, dispositions, asset-for-asset exchanges, and transfers
between the user's own wallets do not all receive the same treatment. The
profile does not decide asset character, taxpayer classification, dealer or
trader status, section 1256 treatment, state conformity, or filing forms.

## Primary and product sources

Recheck these at execution time. The embedded guidance was last reviewed on
2026-08-13.

- [IRS Digital Assets](https://www.irs.gov/filing/digital-assets)
- [IRS Digital Asset Transaction FAQs](https://www.irs.gov/individuals/international-taxpayers/frequently-asked-questions-on-digital-asset-transactions)
- [IRS Revenue Procedure 2024-28](https://www.irs.gov/pub/irs-drop/rp-24-28.pdf)
- [FASB ASU 2023-08](https://storage.fasb.org/ASU%202023-08.pdf)
- [Bitwave Inventory Views](https://docs.bitwave.io/docs/inventory-views-1)

Only U.S. guidance is currently reviewed. For another jurisdiction, the CLI
must say it is unsupported, gather the user's facts, consult current primary
sources, and obtain qualified professional approval before proposing a
profile.
