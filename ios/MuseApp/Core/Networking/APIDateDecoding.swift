import Foundation

public enum RFC3339Date {
    public static func parse(_ text: String) -> Date? {
        guard let tIndex = text.firstIndex(where: { $0 == "T" || $0 == "t" }) else { return nil }
        let afterT = text[text.index(after: tIndex)...]
        guard let zoneStart = afterT.lastIndex(where: { $0 == "Z" || $0 == "z" || $0 == "+" || $0 == "-" }) else {
            return nil
        }
        let date = text[..<tIndex]
        let time = afterT[..<zoneStart]
        let zone = afterT[zoneStart...]

        let seconds: Substring
        let fraction: Substring?
        if let dot = time.firstIndex(of: ".") {
            seconds = time[..<dot]
            fraction = time[time.index(after: dot)...]
        } else {
            seconds = time
            fraction = nil
        }

        guard hasShape(date, "dddd-dd-dd"), hasShape(seconds, "dd:dd:dd"),
              zone == "Z" || zone == "z" || hasShape(zone, "+dd:dd") || hasShape(zone, "-dd:dd")
        else { return nil }

        let formatter = ISO8601DateFormatter()
        formatter.formatOptions = [.withInternetDateTime]
        guard let whole = formatter.date(from: String(date) + "T" + seconds + zone) else { return nil }

        guard let fraction else { return whole }
        guard !fraction.isEmpty, fraction.count <= 9,
              fraction.allSatisfy({ $0.isASCII && $0.isNumber }),
              let digits = Double(String(fraction))
        else { return nil }
        return whole.addingTimeInterval(digits / pow(10, Double(fraction.count)))
    }

    private static func hasShape(_ value: Substring, _ pattern: String) -> Bool {
        guard value.count == pattern.count else { return false }
        return zip(value, pattern).allSatisfy { char, spec in
            spec == "d" ? (char.isASCII && char.isNumber) : (char == spec)
        }
    }
}

extension JSONDecoder.DateDecodingStrategy {
    public static var rfc3339: JSONDecoder.DateDecodingStrategy {
        .custom { decoder in
            let container = try decoder.singleValueContainer()
            let text = try container.decode(String.self)
            guard let date = RFC3339Date.parse(text) else {
                throw DecodingError.dataCorruptedError(
                    in: container,
                    debugDescription: "Expected an RFC 3339 timestamp, got \"\(text)\""
                )
            }
            return date
        }
    }
}
