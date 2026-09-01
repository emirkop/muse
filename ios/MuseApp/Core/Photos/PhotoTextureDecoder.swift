import CoreGraphics
import Foundation
import ImageIO

public enum PhotoTextureDecoder {
    public enum Failure: Error, Equatable {
        case notAnImage
        case decodeFailed
    }

    public static func decode(_ data: Data, maxLongEdge: Int) throws -> DecodedPhotoImage {
        guard let source = CGImageSourceCreateWithData(data as CFData, nil),
              CGImageSourceGetCount(source) > 0 else {
            throw Failure.notAnImage
        }
        let options: [CFString: Any] = [
            kCGImageSourceCreateThumbnailFromImageAlways: true,
            kCGImageSourceCreateThumbnailWithTransform: true,
            kCGImageSourceShouldCacheImmediately: true,
            kCGImageSourceThumbnailMaxPixelSize: maxLongEdge
        ]
        guard let image = CGImageSourceCreateThumbnailAtIndex(source, 0, options as CFDictionary) else {
            throw Failure.decodeFailed
        }
        return DecodedPhotoImage(image: image)
    }
}
