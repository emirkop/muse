import Foundation

public enum RoomPhotoTexturePolicy {
    public static let textureBudgetBytes = 96 * 1_024 * 1_024

    public static let candidateLongEdges = [1_024, 768, 512]

    static let bytesPerTexel = 4
    static let mipmapOverhead = 4.0 / 3.0

    public static func maxLongEdge(forPhotoCount photoCount: Int) -> Int {
        let count = max(photoCount, 1)
        for edge in candidateLongEdges where worstCaseBytes(longEdge: edge) * count <= textureBudgetBytes {
            return edge
        }
        return candidateLongEdges.last!
    }

    public static func worstCaseBytes(longEdge: Int) -> Int {
        Int(Double(longEdge * longEdge * bytesPerTexel) * mipmapOverhead)
    }

    public static func worstCaseRoomBytes(forPhotoCount photoCount: Int) -> Int {
        worstCaseBytes(longEdge: maxLongEdge(forPhotoCount: photoCount)) * max(photoCount, 0)
    }

    public static let maxConcurrentLoads = 3
}
