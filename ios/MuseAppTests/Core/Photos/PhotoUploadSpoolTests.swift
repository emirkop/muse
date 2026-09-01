import XCTest
@testable import MuseApp

final class PhotoUploadSpoolTests: XCTestCase {

    private var spool: PhotoUploadSpool!

    override func setUpWithError() throws {
        let dir = FileManager.default.temporaryDirectory.appendingPathComponent("spool-\(UUID().uuidString)", isDirectory: true)
        spool = PhotoUploadSpool(directory: dir)
        try spool.prepare()
    }

    override func tearDown() {
        try? FileManager.default.removeItem(at: spool.directory)
    }

    func test_prepare_createsTheDirectory_andExcludesItFromBackup() throws {
        var isDirectory: ObjCBool = false
        XCTAssertTrue(FileManager.default.fileExists(atPath: spool.directory.path, isDirectory: &isDirectory))
        XCTAssertTrue(isDirectory.boolValue)

        let values = try spool.directory.resourceValues(forKeys: [.isExcludedFromBackupKey])
        XCTAssertEqual(values.isExcludedFromBackup, true, "normalized copies must not be backed up")
    }

    func test_prepare_isIdempotent() throws {
        try spool.prepare()
        try spool.prepare()
    }

    func test_fileURL_isUniquePerPickedPhoto_andInsideTheSpool() {
        let a = spool.fileURL(forPickedPhotoID: "A")
        let b = spool.fileURL(forPickedPhotoID: "B")

        XCTAssertNotEqual(a, b)
        XCTAssertTrue(a.path.hasPrefix(spool.directory.path))
        XCTAssertEqual(a.pathExtension, "jpg")
    }

    func test_remove_deletesTheFile_andIsIdempotent() throws {
        let url = spool.fileURL(forPickedPhotoID: "done")
        try Data([1, 2, 3]).write(to: url)
        XCTAssertTrue(FileManager.default.fileExists(atPath: url.path))

        spool.remove(url)
        XCTAssertFalse(FileManager.default.fileExists(atPath: url.path))

        spool.remove(url)
    }

    func test_purgeAll_removesEverySpooledFile() throws {
        for id in ["a", "b", "c"] {
            try Data(repeating: 0, count: 10).write(to: spool.fileURL(forPickedPhotoID: id))
        }
        XCTAssertEqual(spool.contents().count, 3)

        spool.purgeAll()

        XCTAssertTrue(spool.contents().isEmpty)
        XCTAssertTrue(FileManager.default.fileExists(atPath: spool.directory.path), "the directory itself stays")
    }

    func test_purgeAll_onAMissingDirectory_isSafe() {
        let ghost = PhotoUploadSpool(directory: FileManager.default.temporaryDirectory.appendingPathComponent("never-\(UUID().uuidString)"))
        ghost.purgeAll()
        XCTAssertTrue(ghost.contents().isEmpty)
    }
}
