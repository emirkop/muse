import CoreGraphics
import CryptoKit
import ImageIO
import UniformTypeIdentifiers
import XCTest
@testable import MuseApp

final class PhotoNormalizerTests: XCTestCase {

    private var outputURL: URL!

    override func setUp() {
        outputURL = FileManager.default.temporaryDirectory.appendingPathComponent("normalized-\(UUID().uuidString).jpg")
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: outputURL)
    }

    // MARK: - Fixtures

    private func sourceJPEG(width: Int, height: Int, orientation: Int = 1, withGPS: Bool = true) throws -> Data {
        let colorSpace = CGColorSpace(name: CGColorSpace.displayP3)!
        let context = CGContext(
            data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
            space: colorSpace, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue
        )!
        context.setFillColor(red: 1, green: 0, blue: 0, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: width / 2, height: height / 2))
        context.setFillColor(red: 0, green: 0, blue: 1, alpha: 1)
        context.fill(CGRect(x: width / 2, y: height / 2, width: width / 2, height: height / 2))
        let image = context.makeImage()!

        var properties: [CFString: Any] = [
            kCGImagePropertyOrientation: orientation,
            kCGImagePropertyExifDictionary: [
                kCGImagePropertyExifLensModel: "Test Lens 26mm",
                kCGImagePropertyExifDateTimeOriginal: "2026:08:25 09:00:00"
            ] as [CFString: Any],
            kCGImagePropertyTIFFDictionary: [
                kCGImagePropertyTIFFMake: "TestPhone",
                kCGImagePropertyTIFFOrientation: orientation
            ] as [CFString: Any]
        ]
        if withGPS {
            properties[kCGImagePropertyGPSDictionary] = [
                kCGImagePropertyGPSLatitude: 41.0082,
                kCGImagePropertyGPSLatitudeRef: "N",
                kCGImagePropertyGPSLongitude: 28.9784,
                kCGImagePropertyGPSLongitudeRef: "E"
            ] as [CFString: Any]
        }

        let data = NSMutableData()
        let destination = CGImageDestinationCreateWithData(data, UTType.jpeg.identifier as CFString, 1, nil)!
        CGImageDestinationAddImage(destination, image, properties as CFDictionary)
        XCTAssertTrue(CGImageDestinationFinalize(destination))
        return data as Data
    }

    private func properties(of url: URL) -> [CFString: Any] {
        let source = CGImageSourceCreateWithURL(url as CFURL, nil)!
        return CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as! [CFString: Any]
    }

    // MARK: - Privacy: metadata stripped

    func test_stripsGPS_EXIF_andTIFFMetadata() throws {
        let source = try sourceJPEG(width: 800, height: 600)
        let sourceProps = CGImageSourceCopyPropertiesAtIndex(CGImageSourceCreateWithData(source as CFData, nil)!, 0, nil) as! [CFString: Any]
        XCTAssertNotNil(sourceProps[kCGImagePropertyGPSDictionary], "fixture must carry GPS for this test to mean anything")

        _ = try PhotoNormalizer.normalize(source, to: outputURL)

        let props = properties(of: outputURL)
        XCTAssertNil(props[kCGImagePropertyGPSDictionary], "GPS must be stripped")
        let exif = props[kCGImagePropertyExifDictionary] as? [CFString: Any] ?? [:]
        XCTAssertNil(exif[kCGImagePropertyExifLensModel], "camera EXIF must be stripped")
        XCTAssertNil(exif[kCGImagePropertyExifDateTimeOriginal], "capture time must be stripped")
        let tiff = props[kCGImagePropertyTIFFDictionary] as? [CFString: Any] ?? [:]
        XCTAssertNil(tiff[kCGImagePropertyTIFFMake], "device make must be stripped")
    }

    // MARK: - Orientation baked in

    func test_bakesOrientationIntoPixels_andLeavesNoOrientationTag() throws {
        let source = try sourceJPEG(width: 800, height: 400, orientation: 6)

        let file = try PhotoNormalizer.normalize(source, to: outputURL)

        XCTAssertEqual(file.pixelWidth, 400, "a 90° rotation swaps the dimensions")
        XCTAssertEqual(file.pixelHeight, 800)
        let props = properties(of: outputURL)
        let orientation = props[kCGImagePropertyOrientation] as? Int
        XCTAssertTrue(orientation == nil || orientation == 1, "no non-identity orientation may remain, got \(String(describing: orientation))")
        XCTAssertEqual(props[kCGImagePropertyPixelWidth] as? Int, 400)
        XCTAssertEqual(props[kCGImagePropertyPixelHeight] as? Int, 800)
    }

    // MARK: - Resolution policy

    func test_downscalesLongEdgeTo3072_preservingAspect() throws {
        let source = try sourceJPEG(width: 4000, height: 2000, withGPS: false)

        let file = try PhotoNormalizer.normalize(source, to: outputURL)

        XCTAssertEqual(file.pixelWidth, 3072)
        XCTAssertEqual(file.pixelHeight, 1536, "aspect ratio must be preserved exactly")
    }

    func test_downscalesPortraitByItsLongEdge() throws {
        let source = try sourceJPEG(width: 1500, height: 4500, withGPS: false)

        let file = try PhotoNormalizer.normalize(source, to: outputURL)

        XCTAssertEqual(file.pixelHeight, 3072)
        XCTAssertEqual(file.pixelWidth, 1024)
    }

    func test_neverUpscales() throws {
        let source = try sourceJPEG(width: 1000, height: 750, withGPS: false)

        let file = try PhotoNormalizer.normalize(source, to: outputURL)

        XCTAssertEqual(file.pixelWidth, 1000)
        XCTAssertEqual(file.pixelHeight, 750)
    }

    // MARK: - Representation

    func test_outputIsAnSRGBJPEG() throws {
        let source = try sourceJPEG(width: 800, height: 600)

        let file = try PhotoNormalizer.normalize(source, to: outputURL)

        XCTAssertEqual(file.contentType, "image/jpeg")
        let imageSource = CGImageSourceCreateWithURL(outputURL as CFURL, nil)!
        XCTAssertEqual(CGImageSourceGetType(imageSource) as String?, UTType.jpeg.identifier)
        let image = CGImageSourceCreateImageAtIndex(imageSource, 0, nil)!
        XCTAssertEqual(image.colorSpace?.name, CGColorSpace.sRGB, "colour space must be normalized to sRGB")
    }

    func test_describesTheWrittenFileExactly() throws {
        let source = try sourceJPEG(width: 800, height: 600)

        let file = try PhotoNormalizer.normalize(source, to: outputURL)

        let written = try Data(contentsOf: outputURL)
        XCTAssertEqual(file.byteSize, written.count)
        XCTAssertEqual(file.fileURL, outputURL)
        XCTAssertEqual(file.sha256Hex.count, 64)
        XCTAssertTrue(file.sha256Hex.allSatisfy { "0123456789abcdef".contains($0) }, "lowercase hex, as the server requires")

        let independent = SHA256.hash(data: written).map { String(format: "%02x", $0) }.joined()
        XCTAssertEqual(file.sha256Hex, independent)
    }

    func test_thumbnail_isSmallJPEGDerivedFromTheSameImage() throws {
        let source = try sourceJPEG(width: 3000, height: 2000, withGPS: false)

        let thumbnail = try PhotoNormalizer.thumbnail(from: source, maxPixelSize: 512)

        let props = CGImageSourceCopyPropertiesAtIndex(CGImageSourceCreateWithData(thumbnail as CFData, nil)!, 0, nil) as! [CFString: Any]
        XCTAssertEqual(props[kCGImagePropertyPixelWidth] as? Int, 512)
        XCTAssertNil(props[kCGImagePropertyGPSDictionary])
    }

    func test_unreadableSource_isARefusal_notACrash() {
        XCTAssertThrowsError(try PhotoNormalizer.normalize(Data("not an image".utf8), to: outputURL)) { error in
            XCTAssertEqual(error as? PhotoNormalizer.Failure, .unreadableSource)
        }
        XCTAssertFalse(FileManager.default.fileExists(atPath: outputURL.path), "nothing may be written for an unreadable source")
    }
}
