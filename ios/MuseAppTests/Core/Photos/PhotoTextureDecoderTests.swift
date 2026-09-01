import CoreGraphics
import ImageIO
import UniformTypeIdentifiers
import XCTest
@testable import MuseApp

final class PhotoTextureDecoderTests: XCTestCase {

    static func jpeg(width: Int, height: Int) -> Data {
        let colorSpace = CGColorSpace(name: CGColorSpace.sRGB)!
        let context = CGContext(data: nil, width: width, height: height, bitsPerComponent: 8, bytesPerRow: 0,
                                space: colorSpace, bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue)!
        context.setFillColor(red: 0.2, green: 0.5, blue: 0.8, alpha: 1)
        context.fill(CGRect(x: 0, y: 0, width: width, height: height))
        let data = NSMutableData()
        let dest = CGImageDestinationCreateWithData(data, UTType.jpeg.identifier as CFString, 1, nil)!
        CGImageDestinationAddImage(dest, context.makeImage()!, [kCGImageDestinationLossyCompressionQuality: 0.8] as CFDictionary)
        CGImageDestinationFinalize(dest)
        return data as Data
    }

    func test_downsamplesLandscapeToTheLongEdge_preservingAspect() throws {
        let decoded = try PhotoTextureDecoder.decode(Self.jpeg(width: 3072, height: 2048), maxLongEdge: 1024)
        XCTAssertEqual(decoded.pixelWidth, 1024)
        XCTAssertEqual(decoded.pixelHeight, 683, "2048 × 1024/3072 = 682.67 → 683")
    }

    func test_downsamplesPortraitByItsLongEdge() throws {
        let decoded = try PhotoTextureDecoder.decode(Self.jpeg(width: 2048, height: 3072), maxLongEdge: 768)
        XCTAssertEqual(decoded.pixelHeight, 768)
        XCTAssertEqual(decoded.pixelWidth, 512)
    }

    func test_neverUpscales() throws {
        let decoded = try PhotoTextureDecoder.decode(Self.jpeg(width: 640, height: 480), maxLongEdge: 1024)
        XCTAssertEqual(decoded.pixelWidth, 640)
        XCTAssertEqual(decoded.pixelHeight, 480)
    }

    func test_notAnImage_throws() {
        XCTAssertThrowsError(try PhotoTextureDecoder.decode(Data("wall".utf8), maxLongEdge: 1024)) { error in
            XCTAssertEqual(error as? PhotoTextureDecoder.Failure, .notAnImage)
        }
    }

    func test_decoding28StoredSources_staysWithinTheTransientMemoryBound() throws {
        let sources = (0..<28).map { index -> Data in
            switch index % 3 {
            case 0: return Self.jpeg(width: 3072, height: 2048)
            case 1: return Self.jpeg(width: 2048, height: 3072)
            default: return Self.jpeg(width: 2400, height: 2400)
            }
        }
        let edge = RoomPhotoTexturePolicy.maxLongEdge(forPhotoCount: 28)

        let before = ResidentMemory.bytes()
        let started = Date()
        var decoded: [DecodedPhotoImage] = []
        for data in sources {
            decoded.append(try PhotoTextureDecoder.decode(data, maxLongEdge: edge))
        }
        let elapsed = Date().timeIntervalSince(started)
        let after = ResidentMemory.bytes()

        XCTAssertEqual(decoded.count, 28)
        for image in decoded {
            XCTAssertLessThanOrEqual(max(image.pixelWidth, image.pixelHeight), edge)
        }
        let growthMB = Double(after - before) / 1_048_576
        XCTAssertLessThan(growthMB, 220, "resident growth \(growthMB) MB suggests full-size decodes")
        print("[measurement] decode 28 × 3072px → \(edge)px: \(String(format: "%.2f", elapsed))s, resident +\(String(format: "%.1f", growthMB)) MB")
    }
}

enum ResidentMemory {
    static func bytes() -> Int {
        var info = mach_task_basic_info()
        var count = mach_msg_type_number_t(MemoryLayout<mach_task_basic_info>.size) / 4
        let result = withUnsafeMutablePointer(to: &info) {
            $0.withMemoryRebound(to: integer_t.self, capacity: 1) {
                task_info(mach_task_self_, task_flavor_t(MACH_TASK_BASIC_INFO), $0, &count)
            }
        }
        return result == KERN_SUCCESS ? Int(info.resident_size) : 0
    }
}
