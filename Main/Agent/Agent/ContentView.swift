//
//  ContentView.swift
//  Agent
//
//  Created by 张峰 on 2026/3/19.
//

import SwiftUI
import AppKit

struct ContentView: View {
    private let serviceRouter = AgentServiceRouter()

    @State private var columnVisibility: NavigationSplitViewVisibility = .all
    @State private var selectedMenu: SidebarMenu? = .chat
    @State private var draftText = ""
    @State private var modelID = "mlx-community/Qwen3.5-0.8B-8bit"
    @State private var sourceModelPath = ModelStore.sourceModelPath
    @State private var importedModelPath = ModelStore.importedModelPath
    @State private var isSending = false
    @State private var isImportingModel = false
    @State private var lastTransport = "N/A"
    @State private var xpcStatus = "Checking..."
    @State private var xpcError = ""
    @State private var messages: [ChatMessage] = [
        ChatMessage(role: .assistant, text: "Connected UI scaffold is ready."),
        ChatMessage(role: .assistant, text: "Send a message and I will call your local agent service.")
    ]

    var body: some View {
        NavigationSplitView(columnVisibility: $columnVisibility) {
            List(SidebarMenu.allCases, selection: $selectedMenu) { item in
                Label(item.title, systemImage: item.icon)
                    .tag(item)
            }
            .navigationTitle("Agent")
            .toolbar {
                ToolbarItem(placement: .navigation) {
                    Button(action: toggleSidebar) {
                        Image(systemName: "sidebar.leading")
                    }
                    .accessibilityLabel("Toggle Sidebar")
                }
            }

        } detail: {
            switch selectedMenu ?? .chat {
            case .chat:
                chatDetail
            case .history:
                PlaceholderView(
                    title: "History",
                    subtitle: "Conversation history module can be added here."
                )
            case .settings:
                settingsDetail
            }
        }
        .navigationSplitViewStyle(.balanced)
        .task {
            await refreshXPCStatus()
        }
    }

    private var chatDetail: some View {
        VStack(spacing: 0) {
            ScrollViewReader { proxy in
                ScrollView {
                    LazyVStack(spacing: 12) {
                        ForEach(messages) { message in
                            MessageBubbleView(message: message)
                                .id(message.id)
                        }
                    }
                    .padding(16)
                }
                .background(Color(nsColor: .windowBackgroundColor))
                .onChange(of: messages.count) { _, _ in
                    guard let lastID = messages.last?.id else { return }
                    withAnimation(.easeOut(duration: 0.2)) {
                        proxy.scrollTo(lastID, anchor: .bottom)
                    }
                }
            }

            Divider()

            if isSending {
                HStack(spacing: 8) {
                    ProgressView()
                        .controlSize(.small)
                    Text("Agent is thinking...")
                        .font(.footnote)
                        .foregroundStyle(.secondary)
                    Spacer()
                }
                .padding(.horizontal, 16)
                .padding(.top, 10)
            }

            HStack(alignment: .bottom, spacing: 12) {
                TextField("Type a message...", text: $draftText, axis: .vertical)
                    .textFieldStyle(.roundedBorder)
                    .lineLimit(1...4)
                    .disabled(isSending)

                Button(action: sendMessage) {
                    Image(systemName: "paperplane.fill")
                }
                .buttonStyle(.borderedProminent)
                .disabled(isSending || draftText.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
            }
            .padding(16)
            .background(.regularMaterial)
        }
        .navigationTitle("Chat")
    }

    private var settingsDetail: some View {
        Form {
            Section("Inference Service") {
                TextField("Model", text: $modelID)
                LabeledContent("Mode", value: "XPC only")
                LabeledContent("Selected Source", value: sourceModelPath)
                LabeledContent("Managed Model", value: importedModelPath)
                Button("Choose Model Folder") {
                    chooseModelFolder()
                }
                if isImportingModel {
                    HStack(spacing: 8) {
                        ProgressView().controlSize(.small)
                        Text("Importing model into App Group store...")
                            .foregroundStyle(.secondary)
                    }
                }
                Button("Test XPC Connection") {
                    Task {
                        await refreshXPCStatus()
                    }
                }
            }
            Section("Current") {
                LabeledContent("Model", value: modelID)
                LabeledContent("Transport", value: lastTransport)
                LabeledContent("XPC Status", value: xpcStatus)
                if !xpcError.isEmpty {
                    LabeledContent("XPC Error", value: xpcError)
                }
                LabeledContent(
                    "App Group Support",
                    value: ModelStore.appSupportRoot?.path ?? "Unavailable (check App Groups capability)"
                )
            }
        }
        .formStyle(.grouped)
        .navigationTitle("Settings")
    }

    private func toggleSidebar() {
        withAnimation(.easeInOut(duration: 0.2)) {
            columnVisibility = (columnVisibility == .all) ? .detailOnly : .all
        }
    }

    private func sendMessage() {
        let content = draftText.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !content.isEmpty else { return }

        messages.append(ChatMessage(role: .user, text: content))
        draftText = ""

        Task {
            await requestAssistantReply()
        }
    }

    @MainActor
    private func requestAssistantReply() async {
        isSending = true
        defer { isSending = false }

        let requestMessages = messages.map { AgentChatMessage(role: $0.role.apiRole, content: $0.text) }
        guard ModelStore.hasImportedModel else {
            xpcStatus = "Unavailable"
            xpcError = "Please import a local model folder in Settings first."
            messages.append(
                ChatMessage(
                    role: .assistant,
                    text: "Please import a local model folder in Settings first."
                )
            )
            return
        }

        do {
            let request = AgentChatRequest(
                model: modelID,
                messages: requestMessages,
                maxTokens: 1024,
                endpoint: "",
            )
            let result = try await serviceRouter.chat(request: request)
            lastTransport = result.transport.rawValue
            xpcStatus = "Connected"
            xpcError = ""

            let text = result.text.trimmingCharacters(in: .whitespacesAndNewlines)
            messages.append(
                ChatMessage(
                    role: .assistant,
                    text: text.isEmpty ? "(Empty response from local agent.)" : text
                )
            )
        } catch {
            xpcStatus = "Unavailable"
            xpcError = error.localizedDescription
            messages.append(
                ChatMessage(
                    role: .assistant,
                    text: "Request failed: \(error.localizedDescription)"
                )
            )
        }
    }

    @MainActor
    private func refreshXPCStatus() async {
        if !ModelStore.hasImportedModel {
            xpcStatus = "Model not imported"
            xpcError = "Please choose and import a local model folder in Settings."
            return
        }

        do {
            try await serviceRouter.ping()
            xpcStatus = "Connected"
            xpcError = ""
        } catch {
            xpcStatus = "Unavailable"
            xpcError = error.localizedDescription
        }
    }

    private func chooseModelFolder() {
        let panel = NSOpenPanel()
        panel.canChooseFiles = false
        panel.canChooseDirectories = true
        panel.allowsMultipleSelection = false
        panel.canCreateDirectories = false
        panel.prompt = "Use This Folder"
        panel.message = "Select a local model folder containing config.json, tokenizer.json, and model.safetensors."

        if panel.runModal() == .OK, let url = panel.url {
            Task {
                await importModelFromSelection(url)
            }
        }
    }

    @MainActor
    private func importModelFromSelection(_ url: URL) async {
        isImportingModel = true
        xpcStatus = "Importing..."
        xpcError = ""
        defer { isImportingModel = false }

        do {
            try ModelStore.importModel(from: url)

            sourceModelPath = ModelStore.sourceModelPath
            importedModelPath = ModelStore.importedModelPath
            await refreshXPCStatus()
        } catch {
            xpcStatus = "Unavailable"
            xpcError = "Failed to import model folder: \(error.localizedDescription)"
        }
    }
}

private enum SidebarMenu: String, CaseIterable, Identifiable {
    case chat
    case history
    case settings

    var id: Self { self }

    var title: String {
        switch self {
        case .chat:
            return "Chat"
        case .history:
            return "History"
        case .settings:
            return "Settings"
        }
    }

    var icon: String {
        switch self {
        case .chat:
            return "bubble.left.and.bubble.right"
        case .history:
            return "clock"
        case .settings:
            return "gearshape"
        }
    }
}

private struct ChatMessage: Identifiable {
    let id = UUID()
    let role: ChatRole
    let text: String
}

private enum ChatRole {
    case user
    case assistant

    var isUser: Bool {
        self == .user
    }

    var apiRole: String {
        switch self {
        case .user:
            return "user"
        case .assistant:
            return "assistant"
        }
    }
}

private struct MessageBubbleView: View {
    let message: ChatMessage

    var body: some View {
        HStack {
            if message.role.isUser {
                Spacer(minLength: 44)
            }

            Text(message.text)
                .padding(.horizontal, 14)
                .padding(.vertical, 10)
                .foregroundStyle(message.role.isUser ? Color.white : Color.primary)
                .background(message.role.isUser ? Color.accentColor : Color(nsColor: .controlBackgroundColor))
                .clipShape(RoundedRectangle(cornerRadius: 14, style: .continuous))

            if !message.role.isUser {
                Spacer(minLength: 44)
            }
        }
        .frame(maxWidth: .infinity, alignment: message.role.isUser ? .trailing : .leading)
    }
}

private struct PlaceholderView: View {
    let title: String
    let subtitle: String

    var body: some View {
        ContentUnavailableView(title, systemImage: "square.stack.3d.up", description: Text(subtitle))
    }
}

#Preview {
    ContentView()
}
