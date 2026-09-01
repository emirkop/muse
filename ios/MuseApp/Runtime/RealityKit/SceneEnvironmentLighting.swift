import CoreGraphics
import RealityKit
import UIKit

/// The environment lighting every RealityKit scene in Muse is lit by.
/// # Why this has to exist
/// Apple's USD feature table states that because RealityKit is an AR-first
/// renderer it "doesn't use any lights included in a USD file, and instead
/// bases its lighting on the scene's real-world lighting" — every `UsdLux` row
/// is blank in the RealityKit column. So **a Room Variant cannot light
/// itself.** No amount of authoring in Blender puts light into the scene, and
/// an exported light is dead weight, which is why the Blender export preset
/// sets `export_lights = False`.
/// Until this type existed, all three scene builders mounted geometry under a
/// bare `AnchorEntity` in `.nonAR` camera mode with **no environment lighting
/// of any kind** — so there was no light source at all, and every authored
/// Room rendered far darker than it was authored.
/// §7 recorded that as a runtime gap rather than an asset defect. This is it
/// being closed.
/// # Why the `ARView` property and not `ImageBasedLightComponent`
/// Both exist in the iOS 26.5 SDK. `ImageBasedLightComponent` (iOS 18+) is
/// entity-scoped and only lights entities carrying an
/// `ImageBasedLightReceiverComponent` pointing at it — which would mean
/// remembering to attach one to every mesh of every loaded bundle (the Gothic
/// Hall alone exports 18), plus every photograph, caption plaque, sculpture,
/// Lobby card and Collection item any later layer mounts. A layer that forgets
/// renders black and nothing fails.
/// `ARView.Environment.ImageBasedLight` is **one assignment per scene** and
/// lights everything in it, present and future. It is not deprecated and is
/// available from iOS 15, comfortably under this target's 18.0. One assignment
/// cannot be forgotten by code that has not been written yet.
/// # Why the environment is GENERATED rather than bundled
/// - **App size.** measured Muse as contributing *zero* resource
/// bytes to the binary — no `Assets.car`, no image, no font. A bundled `.exr`
/// would be the first, and 's 40 MB ceiling exists to be defended in
/// small decisions like this one. This adds nothing: the map is arithmetic.
/// - **There is nothing to bundle.** No authored HDR environment exists
///, and inventing one as a binary blob would put an unreviewable
/// file in the tree where a reviewable function does.
/// - **It is tunable as numbers.** Every value below can be argued about in a
/// diff.
/// # What this deliberately is NOT
/// §4.4 confirms that a Museum Style affects
/// **lighting**, so lighting is ultimately *Style- and Variant-scoped
/// presentation* and belongs in the asset bundle beside the geometry — the
/// bundle contract already carries `material` and `texture` roles.
/// This is **one neutral interior environment for every scene**, deliberately
/// not five, because no authored environment exists to deliver and inventing
/// five would answer a presentation question with no content behind it.
/// The seam is therefore a single function. When a Variant ships its own
/// environment, `resource(for:)` grows a parameter and **no call site changes**.
/// Every constant below is an INTERIM ENGINEERING DEFAULT, not a product
/// decision, and none has been tuned on a physical device.
@MainActor
public enum SceneEnvironmentLighting {

    // MARK: - Tuning (all interim engineering defaults)

    /// Equirectangular latitude-longitude map. 2:1 is the format's definition,
    /// not a choice. 256×128 is small on purpose: RealityKit convolves this
    /// into diffuse and specular probes, so resolution buys almost nothing for
    /// a smooth interior while costing generation time on every cold scene.
    static let mapWidth = 256
    static let mapHeight = 128

    /// A power-of-two exposure offset on the whole environment, RealityKit's
    /// own unit for this. Non-zero because the generated map is an 8-bit LDR
    /// image whose brightest value is ~0.9 linear — dim for a daylit interior,
    /// where a real HDR probe would carry window values far above 1.0. This is
    /// the single number to change if rooms read too dark or too washed out on
    /// a device, and it has NOT been checked on one.
    public static let intensityExponent: Float = 1.25

    /// Authored in sRGB, because that is the colour space the generated
    /// `CGImage` declares and an 8-bit *linear* map bands badly in the darks.
    /// A consequence worth knowing: the decode steepens these ratios, so the
    /// zenith-to-nadir contrast in radiance is greater than it looks here.
    /// The band — window height — is the BRIGHTEST region, not the zenith. A
    /// test caught the first attempt getting that backwards, and it is worth
    /// stating why it matters twice over. Physically, the light in this room
    /// comes from the glazed storey and everything else is lit by it, so a
    /// brighter zenith models a skylight the room does not have. Practically,
    /// light arriving at window height strikes the side walls frontally, which
    /// is what makes 26 of the 28 photographs read; light from straight
    /// overhead merely grazes them and lights the floor.
    /// The nadir is not a throwaway either, and this is the least obvious
    /// number here: **the vault is lit by the LOWER hemisphere.** Its webbing
    /// faces down, so it gathers the floor-bounce term and nothing else — set
    /// the nadir too dark and the star vault, which is the best thing in the
    /// room, disappears.
    private static let zenith = (r: 0.72, g: 0.75, b: 0.82)    // cool, above
    private static let bandPeak = (r: 0.97, g: 0.97, b: 0.95)   // the glazed storey
    private static let horizon = (r: 0.60, g: 0.59, b: 0.56)
    private static let nadir = (r: 0.34, g: 0.30, b: 0.26)      // warm floor bounce

    /// The "window" lobe: a broad band of brighter sky a little above the
    /// horizon, which is where a Gothic hall's glazed storey actually is.
    private static let bandCentreRadians = 0.30
    private static let bandWidthRadians = 0.46

    /// Azimuthal variation is **four-fold** (`cos(4θ)`), and that is the one
    /// non-obvious decision here. A perfectly uniform ring lights every
    /// surface identically, which reads as flat ambient and hides every
    /// moulding, rib and reveal the asset was authored for. Two opposite lobes
    /// would model form better — but RealityKit's mapping from equirectangular
    /// U to a world axis is not stated in the SDK headers and is NOT verified
    /// here, so a two-lobe map might land its bright sides along the nave
    /// instead of across it and rake the very walls holding 26 of the 28
    /// photographs. Four lobes are invariant under the 90° rotations that
    /// ambiguity spans: whichever way round it lands, every wall of a
    /// rectangular room gets both frontal and raking light.
    private static let azimuthLobes = 4.0
    private static let azimuthAmplitude = 0.20

    // MARK: - Application

    private static var cachedResource: EnvironmentResource?

    /// Lights `arView`'s scene. Call once per scene build, before or after
    /// mounting geometry — the environment is not attached to any entity, so
    /// order does not matter.
    /// Failing to build the resource is **not** propagated. An unlit room is a
    /// bad room; a room that refused to open because its lighting could not be
    /// computed would be a broken one, and every other failure path in the
    /// scene builders is already non-fatal.
    public static func apply(to arView: ARView) {
        guard let resource = resource() else { return }
        arView.environment.lighting.resource = resource
        arView.environment.lighting.intensityExponent = intensityExponent
    }

    /// The shared environment. Cached because RealityKit convolves it into
    /// probes on creation, and it is identical for every scene until a Variant
    /// ships its own.
    static func resource() -> EnvironmentResource? {
        if let cachedResource { return cachedResource }
        guard let image = makeEquirectangularImage() else { return nil }
        guard let resource = try? EnvironmentResource(
            equirectangular: image,
            withName: "muse.scene.environment"
        ) else { return nil }
        cachedResource = resource
        return resource
    }

    // MARK: - The map

    /// Builds the equirectangular environment as a plain sRGB bitmap.
    /// Row 0 is the zenith and the last row is the nadir; the horizontal axis
    /// wraps a full turn, so column 0 and the last column must agree or the map
    /// shows a seam. `makeEquirectangularImage` is `internal` precisely so a
    /// test can assert that.
    static func makeEquirectangularImage(width: Int = mapWidth,
                                        height: Int = mapHeight) -> CGImage? {
        guard width > 1, height > 1 else { return nil }
        let bytesPerPixel = 4
        var bytes = [UInt8](repeating: 255, count: width * height * bytesPerPixel)

        for y in 0..<height {
            // Pixel centres, so neither pole is sampled exactly and the top
            // row is not a single degenerate direction.
            let v = (Double(y) + 0.5) / Double(height)
            let elevation = (0.5 - v) * Double.pi     // +pi/2 zenith .. -pi/2 nadir
            for x in 0..<width {
                let azimuth = (Double(x) + 0.5) / Double(width) * 2.0 * Double.pi
                let colour = radiance(elevation: elevation, azimuth: azimuth)
                let offset = (y * width + x) * bytesPerPixel
                bytes[offset] = channel(colour.r)
                bytes[offset + 1] = channel(colour.g)
                bytes[offset + 2] = channel(colour.b)
                bytes[offset + 3] = 255
            }
        }

        guard let provider = CGDataProvider(data: Data(bytes) as CFData),
              let space = CGColorSpace(name: CGColorSpace.sRGB) else { return nil }
        return CGImage(
            width: width,
            height: height,
            bitsPerComponent: 8,
            bitsPerPixel: 8 * bytesPerPixel,
            bytesPerRow: width * bytesPerPixel,
            space: space,
            bitmapInfo: CGBitmapInfo(rawValue: CGImageAlphaInfo.noneSkipLast.rawValue),
            provider: provider,
            decode: nil,
            shouldInterpolate: true,
            intent: .defaultIntent
        )
    }

    private static func channel(_ value: Double) -> UInt8 {
        UInt8(max(0.0, min(1.0, value)) * 255.0 + 0.5)
    }

    /// One direction's colour. Split out so the whole environment is a pure
    /// function of (elevation, azimuth) and can be reasoned about — and tested —
    /// without building a bitmap.
    static func radiance(elevation: Double, azimuth: Double)
        -> (r: Double, g: Double, b: Double) {
        let up = sin(elevation)                       // -1 nadir .. +1 zenith

        // Vertical structure: nadir -> horizon -> zenith, interpolated on the
        // sine of the elevation rather than on the pixel row, so the gradient
        // is uniform per unit of solid angle instead of bunching at the poles.
        let base: (r: Double, g: Double, b: Double)
        if up >= 0 {
            base = mix(horizon, zenith, smoothstep(up))
        } else {
            base = mix(horizon, nadir, smoothstep(-up))
        }

        // The window lobe, brighter on four sides (see `azimuthLobes`).
        let offset = (elevation - bandCentreRadians) / bandWidthRadians
        let band = exp(-offset * offset)
        let lobe = 1.0 + azimuthAmplitude * cos(azimuthLobes * azimuth)
        let weight = band * max(0.0, lobe) / (1.0 + azimuthAmplitude)

        return mix(base, bandPeak, weight)
    }

    /// Hermite ease, so the horizon and the poles are smooth rather than
    /// creased. A crease in an environment map shows up as a hard terminator on
    /// every curved surface in the room.
    private static func smoothstep(_ t: Double) -> Double {
        let x = max(0.0, min(1.0, t))
        return x * x * (3.0 - 2.0 * x)
    }

    private static func mix(_ a: (r: Double, g: Double, b: Double),
                            _ b: (r: Double, g: Double, b: Double),
                            _ t: Double) -> (r: Double, g: Double, b: Double) {
        let k = max(0.0, min(1.0, t))
        return (a.r + (b.r - a.r) * k,
                a.g + (b.g - a.g) * k,
                a.b + (b.b - a.b) * k)
    }
}
