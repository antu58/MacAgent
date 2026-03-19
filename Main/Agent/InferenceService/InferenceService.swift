//
//  InferenceService.swift
//  InferenceService
//
//  Created by 张峰 on 2026/3/19.
//

import Foundation

final class InferenceService: NSObject, InferenceXPCServiceProtocol {
    @objc func ping(_ reply: @escaping (NSString?) -> Void) {
        Task {
            do {
                _ = try await EmbeddedBackendManager.shared.ensureRunning()
                reply(nil)
            } catch {
                reply(error.localizedDescription as NSString)
            }
        }
    }

    @objc func generateReply(_ requestJSON: NSString, reply: @escaping (NSString?, NSString?) -> Void) {
        let raw = requestJSON as String

        Task {
            do {
                let request = try JSONDecoder().decode(XPCRequest.self, from: Data(raw.utf8))
                let endpoint = try await EmbeddedBackendManager.shared.ensureRunning()
                let text = try await requestLocalLLM(endpoint: endpoint, request: request)
                reply(text as NSString, nil)
            } catch {
                reply(nil, error.localizedDescription as NSString)
            }
        }
    }

    private func requestLocalLLM(endpoint: String, request: XPCRequest) async throws -> String {
        guard let url = URL(string: endpoint) else {
            throw XPCError.invalidEndpoint
        }

        let payload = ChatCompletionsRequest(
            model: request.model,
            messages: request.messages,
            max_tokens: request.maxTokens
        )

        var urlRequest = URLRequest(url: url)
        urlRequest.httpMethod = "POST"
        urlRequest.setValue("application/json", forHTTPHeaderField: "Content-Type")
        urlRequest.httpBody = try JSONEncoder().encode(payload)

        let (data, response) = try await URLSession.shared.data(for: urlRequest)
        guard let httpResponse = response as? HTTPURLResponse else {
            throw XPCError.invalidResponse
        }

        guard (200..<300).contains(httpResponse.statusCode) else {
            let body = String(data: data, encoding: .utf8) ?? "unknown error"
            throw XPCError.http(status: httpResponse.statusCode, body: body)
        }

        let decoded = try JSONDecoder().decode(ChatCompletionsResponse.self, from: data)
        return decoded.choices.first?.message.content ?? ""
    }
}

actor EmbeddedBackendManager {
    static let shared = EmbeddedBackendManager()

    private let appGroupID = "group.com.feitianchengzi.Agent"
    private let requiredModelFiles = ["config.json", "tokenizer.json", "model.safetensors"]
    private let host = "127.0.0.1"
    private let port = 18080
    private let startupTimeoutSeconds: TimeInterval = 40

    private var backendProcess: Process?
    private var activeModelPath: String?

    private var chatCompletionsEndpoint: String {
        "http://\(host):\(port)/v1/chat/completions"
    }

    func ensureRunning() async throws -> String {
        let modelURL = try resolveSelectedModelDirectory()

        if await isReachable() {
            return chatCompletionsEndpoint
        }

        if let process = backendProcess, process.isRunning {
            try await waitUntilReachable(timeout: startupTimeoutSeconds)
            if await isReachable() {
                return chatCompletionsEndpoint
            }
        }

        try launchEmbeddedBackend(modelPath: modelURL)
        try await waitUntilReachable(timeout: startupTimeoutSeconds)

        guard await isReachable() else {
            throw XPCError.backendStartupFailed("Backend started but endpoint is still unreachable.")
        }

        return chatCompletionsEndpoint
    }

    private func launchEmbeddedBackend(modelPath: URL) throws {
        let pythonExec = try embeddedPythonExecutableURL()
        let logURL = try backendLogURL()

        let process = Process()
        process.executableURL = pythonExec
        process.arguments = [
            "-m", "mlx_lm.server",
            "--model", modelPath.path,
            "--host", host,
            "--port", String(port),
            "--max-tokens", "1024",
            "--log-level", "INFO",
            "--trust-remote-code",
        ]

        process.environment = try makeRuntimeEnvironment()

        let logHandle = try openLogHandle(logURL: logURL)
        process.standardOutput = logHandle
        process.standardError = logHandle

        process.terminationHandler = { [weak process] _ in
            process?.standardOutput = nil
            process?.standardError = nil
        }

        try process.run()
        backendProcess = process
        activeModelPath = modelPath.path
    }

    private func makeRuntimeEnvironment() throws -> [String: String] {
        var env = ProcessInfo.processInfo.environment
        let cacheRoot = try applicationSupportRootURL()
            .appendingPathComponent("InferenceCache", isDirectory: true)

        try? FileManager.default.createDirectory(at: cacheRoot, withIntermediateDirectories: true)
        let hfHome = cacheRoot.appendingPathComponent("hf", isDirectory: true)
        let hub = hfHome.appendingPathComponent("hub", isDirectory: true)
        let transformers = hfHome.appendingPathComponent("transformers", isDirectory: true)
        let xdg = cacheRoot.appendingPathComponent("xdg-cache", isDirectory: true)
        try? FileManager.default.createDirectory(at: hfHome, withIntermediateDirectories: true)
        try? FileManager.default.createDirectory(at: hub, withIntermediateDirectories: true)
        try? FileManager.default.createDirectory(at: transformers, withIntermediateDirectories: true)
        try? FileManager.default.createDirectory(at: xdg, withIntermediateDirectories: true)

        env["HF_HOME"] = hfHome.path
        env["HUGGINGFACE_HUB_CACHE"] = hub.path
        env["TRANSFORMERS_CACHE"] = transformers.path
        env["XDG_CACHE_HOME"] = xdg.path
        return env
    }

    private func isReachable() async -> Bool {
        guard let url = URL(string: "http://\(host):\(port)/v1/models") else {
            return false
        }

        var req = URLRequest(url: url)
        req.timeoutInterval = 2
        req.httpMethod = "GET"

        do {
            let (_, response) = try await URLSession.shared.data(for: req)
            guard let http = response as? HTTPURLResponse else {
                return false
            }
            return (200..<300).contains(http.statusCode)
        } catch {
            return false
        }
    }

    private func waitUntilReachable(timeout: TimeInterval) async throws {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if await isReachable() {
                return
            }
            try await Task.sleep(nanoseconds: 400_000_000)
        }
        throw XPCError.backendStartupFailed("Timed out waiting for embedded backend startup.")
    }

    private func embeddedPythonExecutableURL() throws -> URL {
        let extractedRoot = try extractedRuntimeRootURL()
        let extractedBinDir = extractedRoot
            .appendingPathComponent("venv", isDirectory: true)
            .appendingPathComponent("bin", isDirectory: true)

        if let python = firstExecutablePython(in: extractedBinDir) {
            return python
        }

        try extractBundledRuntimeArchive()

        if let python = firstExecutablePython(in: extractedBinDir) {
            return python
        }

        throw XPCError.embeddedRuntimeMissing(
            "Embedded runtime python not found after extraction: \(extractedBinDir.path)"
        )
    }

    private func firstExecutablePython(in binDir: URL) -> URL? {
        let candidates = ["python3.11", "python3", "python"]
        for name in candidates {
            let candidate = binDir.appendingPathComponent(name, isDirectory: false)
            if FileManager.default.fileExists(atPath: candidate.path) {
                ensureExecutableBitIfNeeded(candidate)
            }
            if FileManager.default.isExecutableFile(atPath: candidate.path) {
                return candidate
            }
        }
        return nil
    }

    private func extractedRuntimeRootURL() throws -> URL {
        let runtimeRoot = try applicationSupportRootURL()
            .appendingPathComponent("EmbeddedRuntime", isDirectory: true)
        try FileManager.default.createDirectory(at: runtimeRoot, withIntermediateDirectories: true)
        return runtimeRoot
    }

    private func extractBundledRuntimeArchive() throws {
        guard let resourceRoot = Bundle.main.resourceURL else {
            throw XPCError.embeddedRuntimeMissing("Bundle resource root not found.")
        }

        let archiveURL = resourceRoot.appendingPathComponent("venv.tar.gz", isDirectory: false)

        guard FileManager.default.fileExists(atPath: archiveURL.path) else {
            throw XPCError.embeddedRuntimeMissing(
                "Bundled runtime archive missing: \(archiveURL.path)"
            )
        }

        let extractRoot = try extractedRuntimeRootURL()
        let tar = Process()
        tar.executableURL = URL(fileURLWithPath: "/usr/bin/tar")
        tar.arguments = ["-xzf", archiveURL.path, "-C", extractRoot.path]

        let pipe = Pipe()
        tar.standardError = pipe
        try tar.run()
        tar.waitUntilExit()

        if tar.terminationStatus != 0 {
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            let output = String(data: data, encoding: .utf8) ?? ""
            throw XPCError.embeddedRuntimeMissing(
                "Failed to extract bundled runtime archive: \(output)"
            )
        }

        // Some archive flows can lose executable bit; make sure python binaries are runnable.
        let extractedBinDir = extractRoot
            .appendingPathComponent("venv", isDirectory: true)
            .appendingPathComponent("bin", isDirectory: true)
        let pythonNames = ["python3.11", "python3", "python"]
        for name in pythonNames {
            let url = extractedBinDir.appendingPathComponent(name, isDirectory: false)
            ensureExecutableBitIfNeeded(url)
        }
    }

    private func backendLogURL() throws -> URL {
        let logDir = try applicationSupportRootURL().appendingPathComponent("Logs", isDirectory: true)
        try FileManager.default.createDirectory(at: logDir, withIntermediateDirectories: true)
        return logDir.appendingPathComponent("embedded-backend.log", isDirectory: false)
    }

    private func openLogHandle(logURL: URL) throws -> FileHandle {
        if !FileManager.default.fileExists(atPath: logURL.path) {
            FileManager.default.createFile(atPath: logURL.path, contents: nil)
        }
        let handle = try FileHandle(forWritingTo: logURL)
        try handle.seekToEnd()
        return handle
    }

    private func ensureExecutableBitIfNeeded(_ url: URL) {
        let fm = FileManager.default
        guard fm.fileExists(atPath: url.path) else { return }
        if fm.isExecutableFile(atPath: url.path) { return }

        do {
            try fm.setAttributes([.posixPermissions: 0o755], ofItemAtPath: url.path)
        } catch {
            // Keep non-fatal; caller will continue probing and report if still unavailable.
        }
    }

    private func resolveSelectedModelDirectory() throws -> URL {
        let modelURL = try sharedAppSupportRootURL()
            .appendingPathComponent("Models", isDirectory: true)
            .appendingPathComponent("ImportedModel", isDirectory: true)

        var isDir: ObjCBool = false
        if !FileManager.default.fileExists(atPath: modelURL.path, isDirectory: &isDir) || !isDir.boolValue {
            throw XPCError.modelSelectionMissing(
                "Managed model folder not found: \(modelURL.path). Please choose model folder in Settings to import."
            )
        }

        for name in requiredModelFiles {
            let fileURL = modelURL.appendingPathComponent(name, isDirectory: false)
            guard FileManager.default.fileExists(atPath: fileURL.path) else {
                throw XPCError.modelSelectionInvalid("Missing model file: \(fileURL.path)")
            }
        }

        return modelURL
    }

    private func applicationSupportRootURL() throws -> URL {
        let supportRoot = try sharedAppSupportRootURL()
            .appendingPathComponent("InferenceService", isDirectory: true)
        try FileManager.default.createDirectory(at: supportRoot, withIntermediateDirectories: true)
        return supportRoot
    }

    private func sharedAppSupportRootURL() throws -> URL {
        let fm = FileManager.default
        guard let groupRoot = fm.containerURL(forSecurityApplicationGroupIdentifier: appGroupID) else {
            throw XPCError.appGroupUnavailable("App Group container unavailable for \(appGroupID).")
        }

        let supportRoot = groupRoot
            .appendingPathComponent("Library", isDirectory: true)
            .appendingPathComponent("Application Support", isDirectory: true)
        try fm.createDirectory(at: supportRoot, withIntermediateDirectories: true)
        return supportRoot
    }
}

private struct XPCRequest: Decodable {
    let model: String
    let messages: [XPCMessage]
    let maxTokens: Int
    let endpoint: String

    enum CodingKeys: String, CodingKey {
        case model
        case messages
        case maxTokens
        case endpoint
    }
}

private struct XPCMessage: Codable {
    let role: String
    let content: String
}

private struct ChatCompletionsRequest: Encodable {
    let model: String
    let messages: [XPCMessage]
    let max_tokens: Int
}

private struct ChatCompletionsResponse: Decodable {
    let choices: [Choice]

    struct Choice: Decodable {
        let message: Message
    }

    struct Message: Decodable {
        let content: String
    }
}

private enum XPCError: LocalizedError {
    case invalidEndpoint
    case invalidResponse
    case http(status: Int, body: String)
    case embeddedRuntimeMissing(String)
    case backendStartupFailed(String)
    case appGroupUnavailable(String)
    case modelSelectionMissing(String)
    case modelSelectionInvalid(String)

    var errorDescription: String? {
        switch self {
        case .invalidEndpoint:
            return "Invalid endpoint URL in XPC service."
        case .invalidResponse:
            return "Invalid HTTP response in XPC service."
        case let .http(status, body):
            return "HTTP \(status): \(body)"
        case let .embeddedRuntimeMissing(msg):
            return msg
        case let .backendStartupFailed(msg):
            return msg
        case let .appGroupUnavailable(msg):
            return msg
        case let .modelSelectionMissing(msg):
            return msg
        case let .modelSelectionInvalid(msg):
            return msg
        }
    }
}
