//
//  AgentApp.swift
//  Agent
//
//  Created by 张峰 on 2026/3/19.
//

import SwiftUI

@main
struct AgentApp: App {
    init() {
        ModelStore.bootstrap()

        // Warm up XPC backend only when user has imported model into App Group store.
        if ModelStore.hasImportedModel {
            Task {
                do {
                    try await AgentServiceRouter().ping()
                } catch {
                    print("XPC warmup failed: \(error.localizedDescription)")
                }
            }
        }
    }

    var body: some Scene {
        WindowGroup {
            ContentView()
        }
    }
}
