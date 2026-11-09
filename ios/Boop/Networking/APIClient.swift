import Foundation

struct APIError: Error, LocalizedError, Sendable, Equatable {
    let status: Int
    let code: String
    let message: String

    var errorDescription: String? { message }

    static func transport(_ error: any Error) -> APIError {
        APIError(status: 0, code: "unreachable", message: error.localizedDescription)
    }

    var isUnauthorized: Bool { status == 401 }
    var isUnreachable: Bool { status == 0 }
}

/// Typed client for the Boop API. Stateless; the session owns the base URL and credential.
struct APIClient: Sendable {
    let baseURL: URL
    var credential: String?
    var session: URLSession = .shared

    static let decoder: JSONDecoder = {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { decoder in
            let s = try decoder.singleValueContainer().decode(String.self)
            if let date = ISO8601.parse(s) { return date }
            throw DecodingError.dataCorrupted(.init(codingPath: decoder.codingPath, debugDescription: "Bad date \(s)"))
        }
        return d
    }()

    // MARK: Endpoints

    func health() async throws -> Bool {
        struct H: Decodable { let status: String }
        let h: H = try await request("GET", "/health", auth: false)
        return h.status == "ok"
    }

    func exchangePairing(token: String, name: String) async throws -> PairingResult {
        try await request("POST", "/api/v1/pairing/exchange", body: ["token": token, "name": name, "platform": "ios"], auth: false)
    }

    func registerDevice(token: String?, name: String?, bundleID: String) async throws -> Device {
        var body: [String: String] = ["app_bundle_id": bundleID]
        if let token { body["device_token"] = token }
        if let name { body["name"] = name }
        return try await request("POST", "/api/v1/devices", body: body)
    }

    func updateDevice(id: String, name: String) async throws -> Device {
        try await request("PATCH", "/api/v1/devices/\(id)", body: ["name": name])
    }

    func deleteDevice(id: String) async throws {
        let _: Empty = try await request("DELETE", "/api/v1/devices/\(id)")
    }

    func events(project: String? = nil, level: Level? = nil, kind: EventKind? = nil, since: Date? = nil, fingerprint: String? = nil, grouped: Bool = false, before: String? = nil, limit: Int = 50) async throws -> EventPage {
        var items = [URLQueryItem(name: "limit", value: String(limit))]
        items += Self.filterItems(project: project, level: level, since: since)
        if let kind { items.append(.init(name: "kind", value: kind.rawValue)) }
        if let fingerprint, !fingerprint.isEmpty { items.append(.init(name: "fingerprint", value: fingerprint)) }
        if grouped { items.append(.init(name: "grouped", value: "true")) }
        if let before { items.append(.init(name: "before", value: before)) }
        return try await request("GET", "/api/v1/events", query: items)
    }

    /// Event counts by kind for the same filters as `events`, ignoring the kind.
    func eventStats(project: String? = nil, level: Level? = nil, since: Date? = nil) async throws -> EventStats {
        try await request("GET", "/api/v1/events/stats", query: Self.filterItems(project: project, level: level, since: since))
    }

    private static func filterItems(project: String?, level: Level?, since: Date?) -> [URLQueryItem] {
        var items: [URLQueryItem] = []
        if let project, !project.isEmpty { items.append(.init(name: "project", value: project)) }
        if let level { items.append(.init(name: "level", value: level.rawValue)) }
        if let since { items.append(.init(name: "since", value: ISO8601.format(since))) }
        return items
    }

    func event(id: String) async throws -> Event {
        try await request("GET", "/api/v1/events/\(id)")
    }

    // MARK: Plumbing

    private struct Empty: Decodable {}
    private struct ServerError: Decodable { let error: String; let message: String }

    private func request<T: Decodable>(_ method: String, _ path: String, query: [URLQueryItem] = [], body: [String: String]? = nil, auth: Bool = true) async throws -> T {
        var comps = URLComponents(url: baseURL.appending(path: path), resolvingAgainstBaseURL: false)!
        if !query.isEmpty { comps.queryItems = query }
        var req = URLRequest(url: comps.url!)
        req.httpMethod = method
        req.timeoutInterval = 15
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if auth, let credential { req.setValue("Bearer \(credential)", forHTTPHeaderField: "Authorization") }
        if let body {
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
            req.httpBody = try JSONEncoder().encode(body)
        }
        let data: Data
        let response: URLResponse
        do {
            (data, response) = try await session.data(for: req)
        } catch {
            throw APIError.transport(error)
        }
        let status = (response as? HTTPURLResponse)?.statusCode ?? 0
        guard (200..<300).contains(status) else {
            if let e = try? Self.decoder.decode(ServerError.self, from: data) {
                throw APIError(status: status, code: e.error, message: e.message)
            }
            throw APIError(status: status, code: "http_\(status)", message: "Request failed (\(status))")
        }
        if T.self == Empty.self { return Empty() as! T }
        do {
            return try Self.decoder.decode(T.self, from: data)
        } catch {
            throw APIError(status: status, code: "decode", message: "Unexpected response from server")
        }
    }
}

enum ISO8601 {
    nonisolated(unsafe) private static let fractional: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return f
    }()
    nonisolated(unsafe) private static let plain: ISO8601DateFormatter = {
        let f = ISO8601DateFormatter()
        f.formatOptions = [.withInternetDateTime]
        return f
    }()

    static func parse(_ s: String) -> Date? {
        // The server emits nanosecond precision; ISO8601DateFormatter only accepts up to 3 fractional digits.
        var t = s
        if let dot = t.firstIndex(of: "."), let end = t[dot...].firstIndex(where: { $0 == "Z" || $0 == "+" || $0 == "-" }) {
            let frac = t[t.index(after: dot)..<end]
            if frac.count > 3 {
                t.replaceSubrange(dot..<end, with: "." + frac.prefix(3))
            }
        }
        return fractional.date(from: t) ?? plain.date(from: t)
    }

    /// UTC RFC 3339 with seconds, as the server's query filters accept.
    static func format(_ date: Date) -> String {
        plain.string(from: date)
    }
}
