import Foundation

struct Event: Codable, Identifiable, Hashable, Sendable {
    let id: String
    var externalID: String?
    let projectID: String
    let projectName: String
    let projectSlug: String
    let projectIcon: String
    let source: String
    let type: String
    let level: Level
    let title: String
    let body: String
    let fingerprint: String
    let data: JSONValue
    let occurredAt: Date
    let createdAt: Date
    /// True when a silence rule stopped this event from being pushed.
    let silenced: Bool
    /// Buttons that open a URL, shown on the notification and in the detail.
    let actions: [EventAction]
    /// Present in grouped listings: this event is the latest of `group.count` occurrences.
    let group: GroupInfo?
    /// Business kind the server derives from the event (sale, cancellation…).
    let kind: EventKind

    enum CodingKeys: String, CodingKey {
        case id
        case externalID = "external_id"
        case projectID = "project_id"
        case projectName = "project_name"
        case projectSlug = "project_slug"
        case projectIcon = "project_icon"
        case source, type, level, title, body, fingerprint, data
        case occurredAt = "occurred_at"
        case createdAt = "created_at"
        case silenced, actions, group, kind
    }

    init(from decoder: any Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        id = try c.decode(String.self, forKey: .id)
        externalID = try c.decodeIfPresent(String.self, forKey: .externalID)
        projectID = try c.decode(String.self, forKey: .projectID)
        projectName = try c.decodeIfPresent(String.self, forKey: .projectName) ?? ""
        projectSlug = try c.decodeIfPresent(String.self, forKey: .projectSlug) ?? ""
        projectIcon = try c.decodeIfPresent(String.self, forKey: .projectIcon) ?? ""
        source = try c.decodeIfPresent(String.self, forKey: .source) ?? ""
        type = try c.decodeIfPresent(String.self, forKey: .type) ?? ""
        level = Level(lenient: try c.decodeIfPresent(String.self, forKey: .level) ?? "info")
        title = try c.decode(String.self, forKey: .title)
        body = try c.decodeIfPresent(String.self, forKey: .body) ?? ""
        fingerprint = try c.decodeIfPresent(String.self, forKey: .fingerprint) ?? ""
        data = try c.decodeIfPresent(JSONValue.self, forKey: .data) ?? .object([:])
        occurredAt = try c.decode(Date.self, forKey: .occurredAt)
        createdAt = try c.decode(Date.self, forKey: .createdAt)
        silenced = try c.decodeIfPresent(Bool.self, forKey: .silenced) ?? false
        actions = try c.decodeIfPresent([EventAction].self, forKey: .actions) ?? []
        group = try c.decodeIfPresent(GroupInfo.self, forKey: .group)
        kind = EventKind(lenient: try c.decodeIfPresent(String.self, forKey: .kind) ?? "")
    }

    init(id: String, projectID: String, projectName: String, projectIcon: String = "", source: String = "", type: String = "",
         level: Level, title: String, body: String = "", fingerprint: String = "", data: JSONValue = .object([:]),
         occurredAt: Date, createdAt: Date, silenced: Bool = false, actions: [EventAction] = [], group: GroupInfo? = nil, kind: EventKind = .other) {
        self.id = id
        self.externalID = nil
        self.projectID = projectID
        self.projectName = projectName
        self.projectSlug = projectName.lowercased()
        self.projectIcon = projectIcon
        self.source = source
        self.type = type
        self.level = level
        self.title = title
        self.body = body
        self.fingerprint = fingerprint
        self.data = data
        self.occurredAt = occurredAt
        self.createdAt = createdAt
        self.silenced = silenced
        self.actions = actions
        self.group = group
        self.kind = kind
    }

    /// True when this row stands for more than one occurrence.
    var isRepeated: Bool { (group?.count ?? 1) > 1 }

    func hash(into hasher: inout Hasher) { hasher.combine(id) }
    static func == (a: Event, b: Event) -> Bool { a.id == b.id && a.createdAt == b.createdAt }

    /// Rich sections recognised from `data`; everything else is `rest`.
    var sections: EventSections { EventSections(data: data) }
}

struct GroupInfo: Codable, Hashable, Sendable {
    let count: Int
    let firstSeen: Date
    let lastSeen: Date

    enum CodingKeys: String, CodingKey {
        case count
        case firstSeen = "first_seen"
        case lastSeen = "last_seen"
    }
}

struct EventPage: Codable, Sendable {
    let events: [Event]
    let nextCursor: String?

    enum CodingKeys: String, CodingKey {
        case events
        case nextCursor = "next_cursor"
    }
}

/// Business kinds the server sorts events into, in display order: money in,
/// then money at risk and app health.
enum EventKind: String, Codable, CaseIterable, Sendable, Identifiable {
    case sale, renewal, trial, signup, lead
    case paymentFailed = "payment_failed"
    case cancellation, refund, error, other

    var id: String { rawValue }

    static let growth: [EventKind] = [.sale, .renewal, .trial, .signup, .lead]
    static let churn: [EventKind] = [.paymentFailed, .cancellation, .refund, .error, .other]

    /// Plural, for counts and filters.
    var label: String {
        switch self {
        case .sale: "Sales"
        case .renewal: "Renewals"
        case .trial: "Trials"
        case .signup: "Signups"
        case .lead: "Leads"
        case .paymentFailed: "Failed payments"
        case .cancellation: "Cancellations"
        case .refund: "Refunds"
        case .error: "Errors"
        case .other: "Other"
        }
    }

    /// Singular, for the tag on one event.
    var tag: String {
        switch self {
        case .sale: "Sale"
        case .renewal: "Renewal"
        case .trial: "Trial"
        case .signup: "Signup"
        case .lead: "Lead"
        case .paymentFailed: "Payment failed"
        case .cancellation: "Cancellation"
        case .refund: "Refund"
        case .error: "Error"
        case .other: "Other"
        }
    }

    /// Whether a rise is good news (`true`), bad news (`false`) or neither (`nil`).
    var goodWhenUp: Bool? {
        switch self {
        case .sale, .renewal, .trial, .signup, .lead: true
        case .paymentFailed, .cancellation, .refund, .error: false
        case .other: nil
        }
    }

    /// Unknown strings from a newer server decode as `.other` rather than failing.
    init(lenient raw: String) {
        self = EventKind(rawValue: raw) ?? .other
    }
}

/// Counts of one period by kind.
struct KindWindow: Decodable, Sendable, Equatable {
    let total: Int
    let kinds: [String: Int]

    func count(_ kind: EventKind) -> Int { kinds[kind.rawValue] ?? 0 }
}

/// `GET /api/v1/events/stats`: the period's counts and, for a bounded period,
/// the previous equally long one.
struct EventStats: Decodable, Sendable, Equatable {
    let total: Int
    let kinds: [String: Int]
    let previous: KindWindow?
    /// Declared amounts in hundredths by currency and kind: `["BRL": ["sale": 14990]]`.
    let revenueCents: [String: [String: Int]]?

    enum CodingKeys: String, CodingKey {
        case total, kinds, previous
        case revenueCents = "revenue_cents"
    }

    func count(_ kind: EventKind) -> Int { kinds[kind.rawValue] ?? 0 }

    /// Amount of one kind per currency, largest first ("R$ 1.240 · US$ 30"); nil when none.
    func money(_ kind: EventKind) -> String? {
        Self.join((revenueCents ?? [:]).map { ($0.key, $0.value[kind.rawValue] ?? 0) })
    }

    /// Sales plus renewals minus refunds per currency; nil when nothing declared an amount.
    var netRevenue: String? {
        Self.join((revenueCents ?? [:]).map { cur, k in
            (cur, (k["sale"] ?? 0) + (k["renewal"] ?? 0) - (k["refund"] ?? 0))
        })
    }

    private static func join(_ entries: [(String, Int)]) -> String? {
        let parts = entries.filter { $0.1 != 0 }.sorted { abs($0.1) > abs($1.1) }.map { cur, cents in
            (Double(cents) / 100).formatted(.currency(code: cur).precision(.fractionLength(cents % 100 == 0 ? 0 : 2)))
        }
        return parts.isEmpty ? nil : parts.joined(separator: " · ")
    }
}

/// Rolling window shared by the big numbers and the inbox list.
enum StatsPeriod: String, CaseIterable, Sendable, Identifiable {
    case day = "24h", week = "7d", month = "30d", all

    var id: String { rawValue }

    var label: String {
        switch self {
        case .day: "Last 24 hours"
        case .week: "Last 7 days"
        case .month: "Last 30 days"
        case .all: "All time"
        }
    }

    /// Start of the window ending at `now`, or nil for all time.
    func since(_ now: Date) -> Date? {
        switch self {
        case .day: now.addingTimeInterval(-86_400)
        case .week: now.addingTimeInterval(-7 * 86_400)
        case .month: now.addingTimeInterval(-30 * 86_400)
        case .all: nil
        }
    }
}

/// Splits `data` into the sections the detail screen renders specially.
struct EventSections: Sendable {
    static let knownKeys: Set<String> = ["exception", "stacktrace", "tags", "context", "breadcrumbs"]

    var exception: [String: JSONValue]?
    var frames: [StackFrame]?
    var tags: [(String, JSONValue)]?
    var context: [(String, JSONValue)]?
    var breadcrumbs: [Breadcrumb]?
    var rest: [(String, JSONValue)]

    init(data: JSONValue) {
        let obj = data.objectValue ?? [:]
        exception = obj["exception"]?.objectValue
        frames = obj["stacktrace"]?.arrayValue?.map(StackFrame.init(json:))
        tags = obj["tags"]?.objectValue.map { $0.sorted { $0.key < $1.key } }
        context = obj["context"]?.objectValue.map { $0.sorted { $0.key < $1.key } }
        breadcrumbs = obj["breadcrumbs"]?.arrayValue?.map(Breadcrumb.init(json:))
        rest = obj.filter { !Self.knownKeys.contains($0.key) }.sorted { $0.key < $1.key }
    }

    var isEmpty: Bool {
        exception == nil && frames == nil && tags == nil && context == nil && breadcrumbs == nil && rest.isEmpty
    }
}

struct StackFrame: Identifiable, Sendable {
    let id = UUID()
    let function: String
    let file: String
    let line: String?
    let inApp: Bool

    init(json: JSONValue) {
        if let o = json.objectValue {
            function = o["function"]?.display ?? o["module"]?.display ?? "—"
            file = o["file"]?.display ?? o["filename"]?.display ?? ""
            line = o["line"]?.display
            inApp = o["in_app"] == .bool(true)
        } else {
            function = json.display
            file = ""
            line = nil
            inApp = false
        }
    }
}

struct Breadcrumb: Identifiable, Sendable {
    let id = UUID()
    let timestamp: String
    let category: String
    let message: String

    init(json: JSONValue) {
        if let o = json.objectValue {
            timestamp = o["timestamp"]?.display ?? o["time"]?.display ?? ""
            category = o["category"]?.display ?? o["type"]?.display ?? ""
            message = o["message"]?.display ?? o["msg"]?.display ?? json.pretty
        } else {
            timestamp = ""
            category = ""
            message = json.display
        }
    }
}
