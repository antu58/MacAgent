//
//  XPCAgentService.swift
//  Agent
//
//  Created by Codex on 2026/3/19.
//

import Foundation

@objc protocol InferenceXPCServiceProtocol {
    func ping(_ reply: @escaping (NSString?) -> Void)
    func generateReply(_ requestJSON: NSString, reply: @escaping (NSString?, NSString?) -> Void)
}

final class XPCAgentService: AgentChatService {
    // Reserved service name for future bundled XPC Service target.
    private let serviceName = "com.feitianchengzi.Agent.InferenceService"

    func ping() async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            let connection = NSXPCConnection(serviceName: serviceName)
            connection.remoteObjectInterface = NSXPCInterface(with: InferenceXPCServiceProtocol.self)
            connection.invalidationHandler = {}
            connection.interruptionHandler = {}
            connection.resume()

            let proxy = connection.remoteObjectProxyWithErrorHandler { error in
                connection.invalidate()
                continuation.resume(
                    throwing: AgentServiceError.unavailableTransport(
                        "XPC unavailable: \(error.localizedDescription)"
                    )
                )
            } as? InferenceXPCServiceProtocol

            guard let proxy else {
                connection.invalidate()
                continuation.resume(
                    throwing: AgentServiceError.unavailableTransport("XPC proxy not available.")
                )
                return
            }

            proxy.ping { errorText in
                connection.invalidate()
                if let errorText, !errorText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    continuation.resume(throwing: AgentServiceError.unavailableTransport(errorText as String))
                } else {
                    continuation.resume(returning: ())
                }
            }
        }
    }

    func chat(request: AgentChatRequest) async throws -> AgentChatResult {
        let data = try JSONEncoder().encode(request)
        guard let requestJSON = String(data: data, encoding: .utf8) else {
            throw AgentServiceError.invalidResponse
        }

        return try await withCheckedThrowingContinuation { continuation in
            let connection = NSXPCConnection(serviceName: serviceName)
            connection.remoteObjectInterface = NSXPCInterface(with: InferenceXPCServiceProtocol.self)
            connection.invalidationHandler = {}
            connection.interruptionHandler = {}
            connection.resume()

            let proxy = connection.remoteObjectProxyWithErrorHandler { error in
                connection.invalidate()
                continuation.resume(
                    throwing: AgentServiceError.unavailableTransport(
                        "XPC unavailable: \(error.localizedDescription)"
                    )
                )
            } as? InferenceXPCServiceProtocol

            guard let proxy else {
                connection.invalidate()
                continuation.resume(
                    throwing: AgentServiceError.unavailableTransport("XPC proxy not available.")
                )
                return
            }

            proxy.generateReply(requestJSON as NSString) { text, errorText in
                connection.invalidate()

                if let errorText, !errorText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                    continuation.resume(throwing: AgentServiceError.backend(errorText as String))
                    return
                }

                continuation.resume(
                    returning: AgentChatResult(
                        text: (text as String?) ?? "",
                        transport: .xpc
                    )
                )
            }
        }
    }
}
