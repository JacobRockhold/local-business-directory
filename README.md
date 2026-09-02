# Local Business Directory

A coverage-first FileForge plugin that discovers named local businesses through an OpenStreetMap Overpass-compatible endpoint, normalizes and deduplicates the results in its isolated PostgreSQL database, and exposes the current directory to approved Hub applications.

## What the MVP does

- Queues resumable sweeps across as many as 50 latitude/longitude areas at once.
- Searches seven broad business groups by default: shops, trades, offices, commercial amenities, tourism, healthcare, and commercial recreation.
- Runs one source request at a time with administrator-controlled pacing and retry handling.
- Keeps completed requests and saved businesses when a source request fails, so a manual retry resumes at the failed request instead of repeating the sweep.
- Deduplicates by stable OpenStreetMap element identity (`node`, `way`, or `relation`).
- Preserves raw tags and source provenance alongside normalized address/contact fields.
- Provides directory search, CSV export, portable FileForge export/import, and a stable paged backfill API.
- Publishes one completion event per sweep so consumers can backfill changes without receiving thousands of per-record events.

This is intentionally a bulk directory, not a lead-scoring product. It does not rank businesses, scrape websites, infer owners, or collect private data.

## Data-source boundary

The plugin does not use the public Nominatim service. Its UI accepts coordinates directly (or the browser's user-approved location), which avoids prohibited systematic geocoding. The default public Overpass endpoint is appropriate only for moderate, occasional sweeps. Recurring regional collection should use a dedicated or commercial Overpass-compatible endpoint and comply with that provider's limits.

OpenStreetMap-derived records and exports include attribution and ODbL 1.0 license metadata. If you publicly distribute a derived database, review the ODbL share-alike requirements. See [OpenStreetMap copyright and license](https://www.openstreetmap.org/copyright) and the [public Nominatim usage policy](https://operations.osmfoundation.org/policies/nominatim/).

## Provider capability

The plugin provides `com.businesshub.local-businesses.directory.v1` through the Hub bridge. A consuming plugin declares the requirement in its manifest, then an administrator approves the provider and the `POST` method.

Operations:

- `POST /search` — current filtered search with an opaque paging cursor.
- `POST /get` — one current record by stable directory ID.
- `POST /backfill` — stable snapshot pages of up to 1,000 records for downstream projections.
- `POST /discover` — queue an idempotent automated sweep using the administrator-controlled data source.
- `POST /discovery-status` — read a caller-owned automated request using bounded status codes.

The complete contract is in `schemas/directory.v1.openapi.yaml`.

### Connect another plugin

Declare the capability in the consuming plugin's `plugin.yaml`:

```yaml
capabilities:
  provides: []
  requires:
    - id: com.businesshub.local-businesses.directory.v1
      reason: Search and synchronize discovered local businesses.
```

After both plugins are installed, a Hub administrator opens **Connections**, selects the consuming plugin and Local Business Directory, and grants the `POST` operation.

The Hub injects `HUB_SERVICE_URL` and `HUB_SERVICE_TOKEN` into the consumer's runtime. Requests use the capability bridge rather than direct database or container access:

```http
POST {HUB_SERVICE_URL}/capabilities/com.businesshub.local-businesses.directory.v1/search
Authorization: Bearer {HUB_SERVICE_TOKEN}
Content-Type: application/json

{
  "city": "Warrensburg",
  "region": "Missouri",
  "categories": ["shop:bakery", "office:accountant"],
  "limit": 100
}
```

Use `/search` for current filtered queries, `/get` for one current record, and `/backfill` for a stable paged synchronization into another plugin's isolated database. Consumers can subscribe to `com.businesshub.local-businesses.discovery-completed.v1` and start a backfill after each completed sweep. Hub events are delivered at least once, so consumers should deduplicate them by event ID.

## Local development

Clone the [Business Hub repository](https://github.com/JacobRockhold/business-hub) and place this repository at `plugins/local-business-directory`, then run these commands from the Business Hub repository root:

```powershell
pnpm plugin:validate -- --source plugins/local-business-directory
pnpm plugin:build -- --source plugins/local-business-directory --image local-business-directory:0.2.0
pnpm plugin:test -- --source plugins/local-business-directory --image local-business-directory:0.2.0 --database-url <disposable-postgres-url>
```

The database URL passed to `plugin:test` must be disposable and reachable from Docker. Follow the Business Hub [first-plugin guide](https://github.com/JacobRockhold/business-hub/blob/main/docs/plugins/FIRST_PLUGIN.md) to create a signing key, package the image digest, trust the publisher, and install the resulting `.hubpkg`.

## Operational notes

- A sweep has at most 350 area/group requests and a 25 km radius per area. Create multiple jobs for wider coverage.
- Jobs are single-worker and single-request; unfinished jobs resume after a plugin restart.
- Canceling a running job stops it before the next source request. An in-flight request is allowed to finish.
- OpenStreetMap contact and address coverage varies. Verify records before using them for outreach or other consequential operations.
- The Hub database is not shared. Other applications consume this plugin's versioned capability or completion event, preserving FileForge isolation and audit controls.
