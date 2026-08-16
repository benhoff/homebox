# Closet Organization

## Status

Implemented for location pages that are named as a closet/wardrobe or whose direct contents are predominantly recognizable clothing.

## Metadata model

Closet organization uses HomeBox's existing entity types, templates, custom fields, and physical location hierarchy. It does not add clothing-specific database columns.

- `Clothing` is a non-location entity type with a default `Clothing` template.
- The template defines text fields named `Garment type`, `Primary color`, `Size`, `Season`, `Condition`, and `Move disposition`.
- Manufacturer remains the authoritative brand field.
- Locations describe where an item physically sits, such as `Closet > Left rail`; garment categories are metadata rather than locations.
- Item names, descriptions, tags, photos, and existing custom-field values are never rewritten by metadata setup.

The setup action is idempotent. It creates or reuses the Clothing template and entity type, adds only missing fields, preserves populated fields, assigns the Clothing type, and can safely be retried after a partial failure. Garment type, primary color, and season may be initialized from conservative item-name/description rules. Size and condition remain empty when unknown.

## User experience

Closet locations default to a photo-first Closet view while retaining the ordinary Inventory card/table view.

- **Browse** groups garments by garment type, then sorts by color, brand, and name.
- **Declutter** groups garments by move disposition in workflow order: Undecided, Keep for move, Sell, Give away, Donate, Recycle, and Trash.
- Filters cover garment type, primary color, size, season, condition, and brand.
- Cards show the move disposition, brand, and up to four clothing facets. An asterisk identifies a value inferred for display but not yet stored as structured metadata.
- Metadata coverage is visible, and the setup action is offered until every item has stored garment type and primary color values.

## Data-loading requirement

Entity list summaries include manufacturer and custom fields. Location views therefore render and filter organization metadata from one list request rather than fetching every item individually. Full item reads are used only during the explicitly confirmed setup operation before each item update.
