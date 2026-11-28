# Hosting the changeset bundle (proposal)

Where and how the changeset bundle is published is **the maintainers' decision**. This page lists the options
and their costs to inform that decision. It recommends one option but decides nothing.

## What is being hosted

- One directory of `cs-<height>[-<seq>].bin[.zst]` files (about 10,000 blocks per file, ~70 MB each once
  compressed). For mainnet from genesis to about height 16.16M, this is **~102 GB in ~1,600 files**. It grows by
  roughly 70 MB per 10,000 blocks.
- `manifest.json` and `manifest.json.sig` (see [`cmd/manifest`](cmd/manifest/README.md)), plus the public key, which is
  published **out of band** (repo docs, release notes), not next to the bundle.
- Optionally, the verified expected-hash TSV (`expected-all.tsv.zst`, ~0.5 GB). Users can rebuild it with
  `fshdr verify`, so hosting it is only a convenience.

## Integrity does not depend on the host

Whichever host is chosen, users should trust the bundle only through the signed manifest:

1. `manifest verify -dir BUNDLE -manifest manifest.json -sig manifest.json.sig -pub <key from the repo>` checks
   the ed25519 signature, the exact file set (no extra, missing or symlinked files), and every size and sha256.
2. The appliers check every applied height against the hash-chain-verified headers (`fshdr`). A tampered bundle
   that somehow passed step 1 would still fail at the first wrong app hash.

So a host only has to be *available*; it does not have to be *trusted*. That is what makes mirrors, torrents
and third-party hosting acceptable. The signing key matters, though: keep it offline, rotate it by publishing
a new public key in the repo, and sign every bundle release.

## Options

Prices are list prices for the US (checked 2026-11 on the providers' pricing pages: Cloud Storage
<https://cloud.google.com/storage/pricing>, R2 <https://developers.cloudflare.com/r2/pricing/>). Verify them
before deciding: tiers, regions and discounts change. "Per download" means one full 102 GB bundle.

| Option | Storage / month | Cost per full download | Who pays egress | Abuse exposure |
|---|---|---|---|---|
| A. GCS, in-region access only | ~$2.0–2.7 (Standard, $0.020–0.026/GB) | $0 for VMs in the same region; internet egress otherwise | Host (but in-region is free) | Low |
| B. GCS, public, internet egress | ~$2.0–2.7 | **~$12** (~$0.12/GB first-tier internet egress; lower tiers at volume) | Host | **High**: every download costs the host |
| C. GCS, Requester Pays | ~$2.0–2.7 | ~$12, billed to the downloader's GCP project; $0 in-region | Downloader | Low (needs a billing-enabled GCP project) |
| D. Cloudflare R2 (public bucket or custom domain) | ~$1.5 ($0.015/GB; first 10 GB free) | ~$0 (no egress fee; ~1.6k GETs at $0.36/M ≈ $0.001) | Nobody (host pays only storage and ops) | Low cost exposure; still rate-limit the public endpoint |
| E. BitTorrent / IPFS (in addition to one of the above) | seeder disk | $0 to the host after seeding | Peers | Availability depends on seeders |

Notes:

- **A** is what the prototype used: segment VMs and apply VMs in the same region read the bucket for free.
  It suits the multi-VM verification runs (`scripts/orchestrate/`) but is not a public distribution channel.
- **B** has open-ended costs: 100 downloads a month is ~$1,200, and anyone can loop downloads. Avoid unless
  sitting behind a CDN with caching and a budget alert.
- **C** keeps the host's costs flat and is natural for GCP users (it is free for them in-region). The friction:
  downloaders need a GCP project with billing and must pass `--billing-project`, which rules out casual users.
- **D** charges no egress and has an S3-compatible API (`rclone`, `aws s3 --endpoint-url`, plain HTTPS for a
  public bucket). Uploading 102 GB once is ~1.6k Class A operations (well within the free tier). It adds a
  second vendor and account to maintain.
- **E** suits a large, immutable, content-addressed artifact: the manifest already gives per-file sha256 sums.
  A torrent per bundle release, with the R2/GCS URL as a web seed (BEP 19), costs nothing extra and survives
  the host going away. IPFS works similarly but needs pinning (a paid pinning service or a self-run node) to
  stay available.

## Who pays, and abuse

- The publisher (Celestia maintainers or a foundation-run account) pays storage in every option: a few dollars a
  month per bundle. Old bundles can be deleted once a newer bundle covers the same range.
- Egress decides the real cost: B charges the host per download, C charges the downloader, and D and E charge
  nobody.
- Abuse vectors: download loops (B only), hotlinking (B, D), and uploads, which none of these options allow
  (keep buckets read-only to the public, uniform access, no public write). Set a billing budget and alert on
  every option.
- Publishing a bundle means vouching for it. Signatures come only from the release process, never from CI on
  unreviewed branches.

## Recommendation (for the maintainers to accept or reject)

1. **Public distribution: Cloudflare R2** (D) behind a project domain, with a **torrent web-seeded from R2** (E)
   for resilience. The host's cost is about $1.5/month per bundle no matter how many downloads.
2. **Verification and orchestration runs: an in-region GCS bucket** (A) next to the VMs, optionally with
   **Requester Pays** (C) so GCP users can pull it at no cost to the project.
3. In every case: one flattened directory per bundle release, `manifest.json` and `manifest.json.sig` beside it,
   the public key in this repository, and users told to run `manifest verify` before applying.

If a second vendor is unwanted, option C on its own is the low-maintenance fallback.
