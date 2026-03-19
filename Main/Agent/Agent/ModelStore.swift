//
//  ModelStore.swift
//  Agent
//
//  Created by Codex on 2026/3/19.
//

import Foundation

enum ModelStore {
    static let appGroupID = "group.com.feitianchengzi.Agent"
    private static let sourceModelPathKey = "source_model_path"
    private static let importedModelPathKey = "imported_model_path"
    private static let legacyModelPathKey = "selected_model_path"
    private static let legacyBookmarkKey = "selected_model_bookmark_base64"
    private static let requiredModelFiles = ["config.json", "tokenizer.json", "model.safetensors"]

    private static var defaults: UserDefaults? {
        UserDefaults(suiteName: appGroupID)
    }

    static var appSupportRoot: URL? {
        let fm = FileManager.default
        guard let groupRoot = fm.containerURL(forSecurityApplicationGroupIdentifier: appGroupID) else { return nil }
        let appSupport = groupRoot
            .appendingPathComponent("Library", isDirectory: true)
            .appendingPathComponent("Application Support", isDirectory: true)
        try? fm.createDirectory(at: appSupport, withIntermediateDirectories: true)
        return appSupport
    }

    static var sourceModelPath: String {
        defaults?.string(forKey: sourceModelPathKey) ?? "Not selected"
    }

    static var importedModelPath: String {
        defaults?.string(forKey: importedModelPathKey) ?? (importedModelURL?.path ?? "Not imported")
    }

    static var hasImportedModel: Bool {
        guard let url = importedModelURL else { return false }
        return hasRequiredModelFiles(at: url)
    }

    static func importModel(from sourceURL: URL) throws {
        guard let appSupportRoot else {
            throw ModelStoreError.appGroupUnavailable(appGroupID)
        }

        guard hasRequiredModelFiles(at: sourceURL) else {
            throw ModelStoreError.invalidModelFolder(
                "Model folder must contain: \(requiredModelFiles.joined(separator: ", "))"
            )
        }

        let fm = FileManager.default
        let modelsRoot = appSupportRoot
            .appendingPathComponent("Models", isDirectory: true)
        let destination = modelsRoot
            .appendingPathComponent("ImportedModel", isDirectory: true)
        let staging = modelsRoot
            .appendingPathComponent("ImportedModel.tmp", isDirectory: true)

        try fm.createDirectory(at: modelsRoot, withIntermediateDirectories: true)
        try? fm.removeItem(at: staging)

        if destination.path == sourceURL.path {
            try persistModelPaths(sourcePath: sourceURL.path, importedPath: destination.path)
            return
        }

        try fm.copyItem(at: sourceURL, to: staging)
        try? fm.removeItem(at: destination)
        try fm.moveItem(at: staging, to: destination)

        try persistModelPaths(sourcePath: sourceURL.path, importedPath: destination.path)
    }

    static func bootstrap() {
        _ = appSupportRoot != nil
        if let defaults,
           let importedURL = importedModelURL,
           hasRequiredModelFiles(at: importedURL),
           defaults.string(forKey: importedModelPathKey) == nil {
            defaults.set(importedURL.path, forKey: importedModelPathKey)
        }

        // Migrate legacy "selected_model_path" setting to managed model store.
        if !hasImportedModel,
           let defaults,
           let legacyPath = defaults.string(forKey: legacyModelPathKey) {
            let legacyURL = URL(fileURLWithPath: legacyPath, isDirectory: true)
            if hasRequiredModelFiles(at: legacyURL) {
                try? importModel(from: legacyURL)
            }
            defaults.removeObject(forKey: legacyModelPathKey)
            defaults.removeObject(forKey: legacyBookmarkKey)
        }
    }

    private static var importedModelURL: URL? {
        appSupportRoot?
            .appendingPathComponent("Models", isDirectory: true)
            .appendingPathComponent("ImportedModel", isDirectory: true)
    }

    private static func hasRequiredModelFiles(at folder: URL) -> Bool {
        var isDir: ObjCBool = false
        guard FileManager.default.fileExists(atPath: folder.path, isDirectory: &isDir), isDir.boolValue else {
            return false
        }
        return requiredModelFiles.allSatisfy {
            FileManager.default.fileExists(atPath: folder.appendingPathComponent($0, isDirectory: false).path)
        }
    }

    private static func persistModelPaths(sourcePath: String, importedPath: String) throws {
        guard let defaults else {
            throw ModelStoreError.appGroupUnavailable(appGroupID)
        }
        defaults.set(sourcePath, forKey: sourceModelPathKey)
        defaults.set(importedPath, forKey: importedModelPathKey)
        defaults.removeObject(forKey: legacyBookmarkKey)
    }
}

enum ModelStoreError: LocalizedError {
    case appGroupUnavailable(String)
    case invalidModelFolder(String)

    var errorDescription: String? {
        switch self {
        case let .appGroupUnavailable(groupID):
            return "App Group unavailable: \(groupID). Please enable App Groups capability for both targets."
        case let .invalidModelFolder(reason):
            return reason
        }
    }
}
