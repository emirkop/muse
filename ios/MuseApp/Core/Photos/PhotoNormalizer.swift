import CoreGraphics
import CryptoKit
import Foundation
import ImageIO
import UniformTypeIdentifiers

public enum PhotoNormalizer {
    public enum Failure: Error, Equatable {
        case unreadableSource
        case couldNotRender
        case couldNotEncode
        case couldNotWrite
    }

    public static let maxLongEdge = NormalizedPhotoFile.maxLongEdge
    public static let jpegQuality: CGFloat = 0.88

    public static func normalize(_ source: Data, to destination: URL) throws -> NormalizedPhotoFile {
        let image = try renderSRGB(source, maxPixelSize: maxLongEdge)
        try encodeJPEG(image, to: destination)
        return try describe(fileAt: destination, width: image.width, height: image.height)
    }

    public static func thumbnail(from source: Data, maxPixelSize: Int) throws -> Data {
        let image = try renderSRGB(source, maxPixelSize: maxPixelSize)
        let data = NSMutableData()
        guard let destination = CGImageDestinationCreateWithData(data, UTType.jpeg.identifier as CFString, 1, nil) else {
            throw Failure.couldNotEncode
        }
        CGImageDestinationAddImage(destination, image, [kCGImageDestinationLossyCompressionQuality: 0.8] as CFDictionary)
        guard CGImageDestinationFinalize(destination) else { throw Failure.couldNotEncode }
        return data as Data
    }

    // MARK: - Steps

    static func renderSRGB(_ source: Data, maxPixelSize: Int) throws -> CGImage {
        guard let imageSource = CGImageSourceCreateWithData(source as CFData, nil),
              CGImageSourceGetCount(imageSource) > 0 else {
            throw Failure.unreadableSource
        }

        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxPixelSize
        ]
        guard let oriented = CGImageSourceCreateThumbnailAtIndex(imageSource, 0, options as CFDictionary) else {
            throw Failure.unreadableSource
        }

        guard let colorSpace = CGColorSpace(name: CGColorSpace.sRGB),
              let context = CGContext(
                  data: nil,
                  width: oriented.width,
                  height: oriented.height,
                  bitsPerComponent: 8,
                  bytesPerRow: 0,
                  space: colorSpace,
                  bitmapInfo: CGImageAlphaInfo.noneSkipLast.rawValue | CGBitmapInfo.byteOrder32Big.rawValue
              ) else {
            throw Failure.couldNotRender
        }
        context.interpolationQuality = .high
        context.draw(oriented, in: CGRect(x: 0, y: 0, width: oriented.width, height: oriented.height))
        guard let rendered = context.makeImage() else { throw Failure.couldNotRender }
        return rendered
    }

    static func encodeJPEG(_ image: CGImage, to destination: URL) throws {
        guard let encoder = CGImageDestinationCreateWithURL(destination as CFURL, UTType.jpeg.identifier as CFString, 1, nil) else {
            throw Failure.couldNotWrite
        }
        let properties: [CFString: Any] = [
            kCGImageDestinationLossyCompressionQuality: jpegQuality
        ]
        CGImageDestinationAddImage(encoder, image, properties as CFDictionary)
        guard CGImageDestinationFinalize(encoder) else { throw Failure.couldNotEncode }
    }

    static func describe(fileAt url: URL, width: Int, height: Int) throws -> NormalizedPhotoFile {
        guard let handle = try? FileHandle(forReadingFrom: url) else { throw Failure.couldNotWrite }
        defer { try? handle.close() }

        var hasher = SHA256()
        var byteCount = 0
        while true {
            let chunk = try handle.read(upToCount: 512 * 1024) ?? Data()
            if chunk.isEmpty { break }
            hasher.update(data: chunk)
            byteCount += chunk.count
        }
        let digest = hasher.finalize().map { String(format: "%02x", $0) }.joined()

        return NormalizedPhotoFile(
            fileURL: url,
            contentType: NormalizedPhotoFile.contentType,
            byteSize: byteCount,
            pixelWidth: width,
            pixelHeight: height,
            sha256Hex: digest
        )
    }
}
