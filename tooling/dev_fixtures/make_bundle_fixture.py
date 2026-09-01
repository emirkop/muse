#!/usr/bin/env python3
"""Generates the DEVELOPMENT FIXTURE asset bundle used to validate Muse's
asset-delivery architecture.

WHAT THIS IS NOT
================
This is **not** production artwork, not a Museum Style, not a Room
Variant, and not visual direction of any kind. It emits an untextured
grey box with a labelled, obviously-synthetic identity, for exactly one
purpose: exercising the versioned publish → CDN → resumable download →
integrity-verify → RealityKit-load contract before any authored 3D
content exists.

Final Museums, Room Variants, sculptures, avatars and Collection
environments are authored in Blender by the product owner
. When that content exists it replaces this
fixture by being *published* under a real bundle identity — no delivery
code changes. That is the property this fixture exists to prove.

Blender is deliberately NOT required or installed to run this: the
geometry is emitted directly as USD ASCII (`.usda`), which RealityKit
loads natively (: USD/USDZ, verified this phase against
`Entity(contentsOf:)`).

USAGE
=====
    python3 tooling/dev_fixtures/make_bundle_fixture.py \
        --out assets/dev_fixtures/bundles

Emits, for each fixture version, a directory containing:
    bundle.json    — the publish descriptor `cmd/assetpublish` reads
    geometry.usda  — the environment shell (role: geometry)
    layout.json    — the slot→transform table + entry point (role: layout)

The bundle contract those three files satisfy is specified in
. Only stdlib is used.
"""

from _future_ import annotations

import argparse
import json
import math
import os
from dataclasses import dataclass

# The fixture's identities. Deliberately shaped so they can never be
# mistaken for a catalog Style/Variant id (which look like
# `style_modern` / `style_modern_variant_Hall`), matching the same
# discipline `PlaceholderRoomSlotTable.variantID` already uses.
BUNDLE_ID = "dev_fixture_room_variant"
VARIANT_ID = "dev-fixture:room-variant"

# Collection Design fixture. A second *identity* through the
# same generator and the same delivery pipeline — not a second pipeline.
# Shaped so it cannot be mistaken for a catalog Design id either, and so
# it cannot be mistaken for the Room Variant fixture above.
COLLECTION_DESIGN_BUNDLE_ID = "dev_fixture_collection_design"
#. Must match `catalog/domain.SeedCollectionModels`'s reference for
# `dev-fixture:model-chrono-one` — the mapping is the point of this fixture,
# so the id is not free to differ.
COLLECTION_MODEL_BUNDLE_ID = "dev_fixture_collection_model"
COLLECTION_DESIGN_ID = "dev-fixture:collection-design"

# tier table. **ENGINEERING FIXTURE VALUES, NOT PRODUCT
# DECISIONS** — is explicitly still OPEN, and the product owner's
# instruction is that final capacities are authored per Design
# against real artwork.
# Chosen only to be small enough that a test can cross two thresholds
# quickly, and large enough that crossing one means something. Nothing
# about a real collection's shape is implied by 4, 10 or 18.
# Cumulative, not incremental: `02`'s trigger is "adding an item would
# exceed the current layout's capacity", which is a total.
COLLECTION_DESIGN_TIERS = (
    # (tier, cumulative_capacity, additional geometry bundle or None)
    (1, 4, None),
    (2, 10, COLLECTION_DESIGN_BUNDLE_ID + "_tier2"),
    (3, 18, COLLECTION_DESIGN_BUNDLE_ID + "_tier3"),
)

# Every anchor a full 28-photo Room needs: 1 focal + 13 left + 13 right
# + 1 rear (/). Authored here so the fixture can render a
# Room at every supported photo count from 1 to 28.
SIDE_WALL_POSITIONS = 13


@dataclass(frozen=True)
class RoomShell:
    """The fixture's dimensions, in metres (RealityKit's unit, Y up)."""

    width: float       # X
    depth: float       # Z
    height: float      # Y
    thickness: float
    mount_height: float
    side_envelope: tuple[float, float, float]
    end_envelope: tuple[float, float, float]
    colour: tuple[float, float, float]

    @property
    def wall_offset(self) -> float:
        """Stand-off from the wall surface, so a photo plane never
        z-fights the wall it hangs on."""
        return self.thickness / 2 + 0.01


# v1 and v2 are the same fixture re-authored, which is exactly the shape
# of a real content update: the geometry changes, the layout changes with
# it, and the bundle version increments. v2 is a wider, taller room in a
# different grey — visibly different on screen and byte-different on
# disk, so a version bump is verifiable by eye and by checksum.
VERSIONS: dict[int, RoomShell] = {
    1: RoomShell(
        width=7.0,
        depth=9.0,
        height=3.0,
        thickness=0.15,
        mount_height=1.55,
        side_envelope=(0.55, 0.55, 1.0),
        end_envelope=(1.6, 1.2, 1.0),
        colour=(0.58, 0.58, 0.60),
    ),
    2: RoomShell(
        width=7.6,
        depth=9.0,
        height=3.4,
        thickness=0.15,
        mount_height=1.60,
        side_envelope=(0.55, 0.55, 1.0),
        end_envelope=(1.7, 1.3, 1.0),
        colour=(0.44, 0.45, 0.48),
    ),
}


# MARK: - USD emission


def yaw_quaternion(radians: float) -> list[float]:
    """A rotation about Y as [x, y, z, w] — the order the layout file and
    `simd_quatf(ix:iy:iz:r:)` both use."""
    return [0.0, round(math.sin(radians / 2), 7), 0.0, round(math.cos(radians / 2), 7)]


def box_mesh(name: str, centre: tuple[float, float, float], size: tuple[float, float, float],
             colour: tuple[float, float, float]) -> str:
    """One axis-aligned box as a USD Mesh prim.

    Authored as a *separate prim per surface* rather than one closed
    volume, for a load-bearing reason: the runtime derives collision from
    the loaded entity's model meshes, and a single convex hull around a
    whole room would be a solid block with the viewer trapped inside it.
    Five boxes give five collidable surfaces.

    `doubleSided = 1` because a Room is viewed from the inside; without
    it the walls are back-face culled and the room looks open.
    """
    cx, cy, cz = centre
    hx, hy, hz = size[0] / 2, size[1] / 2, size[2] / 2

    points = [
        (cx - hx, cy - hy, cz + hz),  # 0 bottom front-left
        (cx + hx, cy - hy, cz + hz),  # 1 bottom front-right
        (cx + hx, cy - hy, cz - hz),  # 2 bottom back-right
        (cx - hx, cy - hy, cz - hz),  # 3 bottom back-left
        (cx - hx, cy + hy, cz + hz),  # 4 top front-left
        (cx + hx, cy + hy, cz + hz),  # 5 top front-right
        (cx + hx, cy + hy, cz - hz),  # 6 top back-right
        (cx - hx, cy + hy, cz - hz),  # 7 top back-left
    ]
    # Six quads, wound outward.
    faces = [
        (0, 1, 2, 3),  # bottom
        (7, 6, 5, 4),  # top
        (0, 4, 5, 1),  # front (+Z)
        (1, 5, 6, 2),  # right (+X)
        (2, 6, 7, 3),  # back (-Z)
        (3, 7, 4, 0),  # left (-X)
    ]

    point_list = ", ".join(f"({p[0]:.4f}, {p[1]:.4f}, {p[2]:.4f})" for p in points)
    index_list = ", ".join(str(i) for face in faces for i in face)
    return f"""    def Mesh "{name}"
    {{
        uniform bool doubleSided = 1
        float3[] extent = [({cx - hx:.4f}, {cy - hy:.4f}, {cz - hz:.4f}), ({cx + hx:.4f}, {cy + hy:.4f}, {cz + hz:.4f})]
        int[] faceVertexCounts = [4, 4, 4, 4, 4, 4]
        int[] faceVertexIndices = [{index_list}]
        point3f[] points = [{point_list}]
        color3f[] primvars:displayColor = [({colour[0]}, {colour[1]}, {colour[2]})]
        uniform token subdivisionScheme = "none"
    }}
"""


def geometry_usda(shell: RoomShell, version: int) -> str:
    half_w, half_d = shell.width / 2, shell.depth / 2
    t = shell.thickness
    wall_y = shell.height / 2

    prims = [
        box_mesh("Floor", (0, -t / 2, 0), (shell.width, t, shell.depth), shell.colour),
        box_mesh("WallLeft", (-half_w, wall_y, 0), (t, shell.height, shell.depth), shell.colour),
        box_mesh("WallRight", (half_w, wall_y, 0), (t, shell.height, shell.depth), shell.colour),
        box_mesh("WallFocal", (0, wall_y, -half_d), (shell.width, shell.height, t), shell.colour),
        box_mesh("WallRear", (0, wall_y, half_d), (shell.width, shell.height, t), shell.colour),
    ]
    body = "\n".join(prims)
    return f"""#usda 1.0
(
    doc = "MUSE DEVELOPMENT FIXTURE v{version} - NOT production artwork, NOT a Room Variant. Generated by tooling/dev_fixtures/make_bundle_fixture.py to validate asset delivery. See."
    defaultPrim = "DevFixtureRoom"
    metersPerUnit = 1
    upAxis = "Y"
)

def Xform "DevFixtureRoom"
{{
{body}}}
"""


# MARK: - Layout emission


def transform(position: tuple[float, float, float], yaw: float,
              scale: tuple[float, float, float]) -> dict:
    return {
        "position": [round(v, 4) for v in position],
        "rotation": yaw_quaternion(yaw),
        "scale": [round(v, 4) for v in scale],
    }


def layout_json(shell: RoomShell, version: int) -> dict:
    half_w, half_d = shell.width / 2, shell.depth / 2
    offset = shell.wall_offset
    y = shell.mount_height

    photos = [
        # Focal wall: the wall opposite the entrance, at -Z, facing +Z.
        dict(wall="focal", position_on_wall=0,
             **transform((0, y, -half_d + offset), 0.0, shell.end_envelope)),
        # Rear wall: beside the entrance, at +Z, facing -Z.
        dict(wall="rear", position_on_wall=0,
             **transform((0, y, half_d - offset), math.pi, shell.end_envelope)),
    ]

    # Side walls fill entrance-outward (+Z towards -Z), matching
    # `SlotAnchor.positionOnWall`'s definition.
    usable_depth = shell.depth - 0.6
    pitch = usable_depth / SIDE_WALL_POSITIONS
    for position in range(SIDE_WALL_POSITIONS):
        z = half_d - 0.3 - pitch * (position + 0.5)
        # A plane's front is +Z; +pi/2 about Y turns it to +X, -pi/2 to -X.
        photos.append(dict(wall="left", position_on_wall=position,
                           **transform((-half_w + offset, y, z), math.pi / 2, shell.side_envelope)))
        photos.append(dict(wall="right", position_on_wall=position,
                           **transform((half_w - offset, y, z), -math.pi / 2, shell.side_envelope)))

    inset = 1.1
    sculpture_envelope = (0.7, 1.4, 0.7)
    sculptures = [
        dict(slot_index=0, **transform((-half_w + inset, 0, -half_d + inset), 0.0, sculpture_envelope)),
        dict(slot_index=1, **transform((half_w - inset, 0, -half_d + inset), 0.0, sculpture_envelope)),
        dict(slot_index=2, **transform((-half_w + inset, 0, 0), 0.0, sculpture_envelope)),
    ]

    return {
        "_comment": (
            "MUSE DEVELOPMENT FIXTURE - NOT production artwork. Generated by "
            "tooling/dev_fixtures/make_bundle_fixture.py. Contract: "
            ""
        ),
        "format_version": 1,
        "variant_id": VARIANT_ID,
        # `02`'s Navigation Principle: entry is always at a defined
        # entrance, never mid-space. Authored with the geometry, because
        # only the geometry's author knows where its entrance is.
        "entry": {"position": [0.0, 0.0, round(half_d - 1.0, 4)], "yaw": 0.0},
        "photo_transforms": photos,
        "sculpture_transforms": sculptures,
    }


def bundle_json(version: int) -> dict:
    return {
        "_comment": (
            "MUSE DEVELOPMENT FIXTURE publish descriptor - NOT production artwork."
        ),
        "bundle_id": BUNDLE_ID,
        "version": version,
        "kind": "room_variant",
        # The runtime format of the geometry file. `.usda` is USD's ASCII
        # form, which RealityKit loads directly; production art ships as
        # `.usdz`. The delivery path never inspects this — it
        # is compatibility metadata, which is exactly why swapping the
        # fixture for real art is a data change, not a code change.
        "format": "usda",
        "min_app_version": 1,
        "files": [
            {"asset_id": "geometry", "role": "geometry",
             "path": "geometry.usda", "content_type": "model/vnd.usda+ascii"},
            {"asset_id": "layout", "role": "layout",
             "path": "layout.json", "content_type": "application/json"},
        ],
        "dependencies": [],
    }


def collection_design_bundle_json() -> dict:
 """Collection Design fixture descriptor.

    GEOMETRY ONLY, and the absence of `layout.json` is deliberate rather
    than lazy. A Collection Design's layout is its per-tier item display
 slots, and both the tier thresholds and whether a tier
 ever retracts are OPEN product/architecture decisions
 belonging to. Authoring a slot table here would answer them
    by accident, in a fixture, which is the worst possible place for a
    product decision to be made.

    The bundle contract permits this: exactly one geometry file is
    required, every other role is optional
.
    """
    return {
        "_comment": (
            "MUSE DEVELOPMENT FIXTURE publish descriptor - NOT production artwork, "
            "NOT a Collection Room design, NOT visual direction. Geometry only: a "
            "Design's per-tier slot table is (, both OPEN)."
        ),
        "bundle_id": COLLECTION_DESIGN_BUNDLE_ID,
        "version": 1,
        "kind": "collection_design",
        "format": "usda",
        "min_app_version": 1,
        "files": [
            {"asset_id": "geometry", "role": "geometry",
             "path": "geometry.usda", "content_type": "model/vnd.usda+ascii"},
            #: the tier table. Geometry-only w honest
            # state ( and were both open); is now
            # closed and the capacities are authored here as labelled
            # fixture values.
            {"asset_id": "layout", "role": "layout",
             "path": "layout.json", "content_type": "application/json"},
        ],
        "dependencies": [],
    }


def write_room_variant_fixture(out: str) -> None:
    for version, shell in sorted(VERSIONS.items()):
        directory = os.path.join(out, BUNDLE_ID, f"v{version}")
        os.makedirs(directory, exist_ok=True)

        with open(os.path.join(directory, "geometry.usda"), "w", encoding="utf-8") as handle:
            handle.write(geometry_usda(shell, version))
        with open(os.path.join(directory, "layout.json"), "w", encoding="utf-8") as handle:
            json.dump(layout_json(shell, version), handle, indent=2)
            handle.write("\n")
        with open(os.path.join(directory, "bundle.json"), "w", encoding="utf-8") as handle:
            json.dump(bundle_json(version), handle, indent=2)
            handle.write("\n")

        print(f"wrote {directory}")


def collection_design_layout_json() -> dict:
 """tier table, authored beside the geometry.

    This is the `layout` role for a Collection Design, and it is where
 's capacities live: the instruction is that *the
    Design bundle supplies authored cumulative capacity per tier*, which
    matches `04`'s Collection Room Expansion ("the active Design's asset
    bundle defines, per tier, the additional geometry to reveal and the
    slot->transform table for the newly available slots").

    The database therefore holds no capacity table at all. It holds only a
    `tier_count` bound, so the server can refuse a tier beyond what a
    Design authors without knowing what any tier holds.

    Per tier: the cumulative capacity, the transforms for the slots that
    tier ADDS (never a repeat of earlier tiers'), and the optional bundle
    carrying the additional geometry that tier reveals. Tier 1's geometry
    is this bundle's own.
    """
    shell = VERSIONS[1]
    half_w, half_d = shell.width / 2, shell.depth / 2
    y = shell.mount_height

    tiers = []
    previous_capacity = 0
    for tier, cumulative, geometry_bundle in COLLECTION_DESIGN_TIERS:
        added = cumulative - previous_capacity
        previous_capacity = cumulative

        # Slots for the surfaces this tier adds, spread along a wall at a
        # deliberate, uninteresting spacing. Placement here is arithmetic,
        # not design: a real Design's slots are authored with its geometry.
        transforms = []
        for index in range(added):
            fraction = (index + 1) / (added + 1)
            transforms.append({
                "slot_index": previous_capacity - added + index,
                "position": [
                    round(-half_w + shell.width * fraction, 4),
                    y,
                    round(-half_d + shell.wall_offset + (tier - 1) * 1.5, 4),
                ],
                "yaw": 0.0,
                "scale": list(shell.end_envelope),
            })

        entry = {
            "tier": tier,
            "cumulative_capacity": cumulative,
            "item_transforms": transforms,
        }
        if geometry_bundle is not None:
            # Incremental delivery: entering this tier installs THIS bundle
            # and nothing else. The model permits it directly —
            # a bundle carries exactly one geometry file, so additional
            # per-tier geometry has to be its own bundle, which is what
            # makes "no full re-download per expansion" structural.
            entry["geometry_bundle"] = {"id": geometry_bundle, "version": 1}
        tiers.append(entry)

    return {
        "_comment": (
            "MUSE DEVELOPMENT FIXTURE - NOT production artwork and NOT product "
            "decisions. The capacities below (4/10/18) are ENGINEERING FIXTURE "
            "VALUES chosen to exercise tier crossing; (real per-Design "
            "capacities) is still OPEN and depends on real artwork."
        ),
        "format_version": 1,
        "design_id": COLLECTION_DESIGN_ID,
        # `02`'s Navigation Principle: entry at a defined entrance, never
        # mid-space. Authored with the geometry.
        "entry": {"position": [0.0, 0.0, round(half_d - 1.0, 4)], "yaw": 0.0},
        "tiers": tiers,
    }


def collection_design_tier_bundle_json(bundle_id: str, tier: int) -> dict:
    """A tier's ADDITIONAL geometry, as its own bundle.

    Geometry only, like the base bundle: the slot table for these surfaces
    lives in the base bundle's `layout.json`, so a client that has already
    installed the base has everything it needs to place items the moment
    this arrives.
    """
    return {
        "_comment": (
            "MUSE DEVELOPMENT FIXTURE publish descriptor - NOT production artwork. "
            "The additional geometry tier %d reveals." % tier
        ),
        "bundle_id": bundle_id,
        "version": 1,
        "kind": "collection_design",
        "format": "usda",
        "min_app_version": 1,
        "files": [
            {"asset_id": "geometry", "role": "geometry",
             "path": "geometry.usda", "content_type": "model/vnd.usda+ascii"},
        ],
        "dependencies": [],
    }


def collection_model_bundle_json() -> dict:
 """Collection Item (catalog Model) fixture descriptor.

    GEOMETRY ONLY, and **NOT visually representative of anything**. phase
    65's product-owner decision defers every piece of final visual work —
    Collection Item presentation, materials, lighting, shadows, mounting
    aesthetics and scale tuning — so this fixture exists for exactly one
    reason: to prove that the Catalog Model -> Presentation Asset mapping
    and its storage/delivery contract work end to end. It expresses no
    opinion about what a collected object looks like, and it must never be
 reviewed against §5's quality bar or
    promoted into `assets/blender/`.
    """
    return {
        "_comment": (
            "MUSE DEVELOPMENT FIXTURE publish descriptor - NOT production artwork, "
            "NOT a collection item, NOT visual direction, and deliberately NOT "
            "visually representative. It exists only to prove the catalog "
            "Model to Presentation Asset mapping and delivery contract."
        ),
        "bundle_id": COLLECTION_MODEL_BUNDLE_ID,
        "version": 1,
        "kind": "collection_item",
        "format": "usda",
        "min_app_version": 1,
        "files": [
            {"asset_id": "geometry", "role": "geometry",
             "path": "geometry.usda", "content_type": "model/vnd.usda+ascii"},
        ],
        "dependencies": [],
    }


def collection_model_geometry_usda() -> str:
    """One small untextured box. Not a watch, not a car, not a coin.

    Deliberately the crudest thing the generator can emit: a single prim
    with the same flat placeholder colour every other fixture uses. A
    fixture that *looked* like a product would invite exactly the visual
 review defers.
    """
    return (
        '#usda 1.0\n'
        '(\n'
 ' doc = "MUSE DEVELOPMENT FIXTURE - NOT production artwork. Proves the '
        'catalog Model to Presentation Asset mapping only; not visually representative."\n'
        '    metersPerUnit = 1\n'
        '    upAxis = "Y"\n'
        ')\n'
        '\n'
        'def Xform "CollectionModelFixture"\n'
        '{\n'
        + box_mesh("Placeholder", (0, 0.05, 0), (0.1, 0.1, 0.1), (0.55, 0.55, 0.58))
        + '}\n'
    )


def write_collection_model_fixture(out: str) -> None:
    """One version, geometry only. See collection_model_bundle_json."""
    directory = os.path.join(out, COLLECTION_MODEL_BUNDLE_ID, "v1")
    os.makedirs(directory, exist_ok=True)
    with open(os.path.join(directory, "geometry.usda"), "w", encoding="utf-8") as handle:
        handle.write(collection_model_geometry_usda())
    with open(os.path.join(directory, "bundle.json"), "w", encoding="utf-8") as handle:
        json.dump(collection_model_bundle_json(), handle, indent=2)
        handle.write("\n")
    print(f"wrote {directory}")


def write_collection_design_fixture(out: str) -> None:
 """One version only. already proved version supersession on
    the Room Variant fixture; repeating it here would add a second copy
    of a property that is already covered."""
    directory = os.path.join(out, COLLECTION_DESIGN_BUNDLE_ID, "v1")
    os.makedirs(directory, exist_ok=True)

    # The same untextured grey box the Room Variant fixture uses, from the
    # same function. A Collection Design's real environment is a display
    # space rather than a photo gallery, but nothing about that is known
    # (`03`: the catalog of design options is undefined), so the fixture
    # deliberately expresses no opinion.
    with open(os.path.join(directory, "geometry.usda"), "w", encoding="utf-8") as handle:
        handle.write(geometry_usda(VERSIONS[1], 1))
    with open(os.path.join(directory, "bundle.json"), "w", encoding="utf-8") as handle:
        json.dump(collection_design_bundle_json(), handle, indent=2)
        handle.write("\n")
    #: the tier table, authored beside the geometry.
    with open(os.path.join(directory, "layout.json"), "w", encoding="utf-8") as handle:
        json.dump(collection_design_layout_json(), handle, indent=2)
        handle.write("\n")

    print(f"wrote {directory}")

    # One bundle per tier that reveals additional geometry, so entering a
    # tier installs only what that tier adds.
    for tier, _capacity, bundle_id in COLLECTION_DESIGN_TIERS:
        if bundle_id is None:
            continue
        tier_directory = os.path.join(out, bundle_id, "v1")
        os.makedirs(tier_directory, exist_ok=True)
        with open(os.path.join(tier_directory, "geometry.usda"), "w", encoding="utf-8") as handle:
            handle.write(geometry_usda(VERSIONS[1], tier))
        with open(os.path.join(tier_directory, "bundle.json"), "w", encoding="utf-8") as handle:
            json.dump(collection_design_tier_bundle_json(bundle_id, tier), handle, indent=2)
            handle.write("\n")
        print(f"wrote {tier_directory}")


def main() -> None:
    parser = argparse.ArgumentParser(description=_doc_)
    parser.add_argument(
        "--out", default="assets/dev_fixtures/bundles",
        help="directory the fixture bundle versions are written under",
    )
    parser.add_argument(
        "--kind", default="all",
        choices=("all", "room_variant", "collection_design", "collection_item"),
        help="which fixture identity to emit",
    )
    args = parser.parse_args()

    if args.kind in ("all", "room_variant"):
        write_room_variant_fixture(args.out)
    if args.kind in ("all", "collection_design"):
        write_collection_design_fixture(args.out)
    if args.kind in ("all", "collection_item"):
        write_collection_model_fixture(args.out)


if _name_ == "_main_":
    main()
