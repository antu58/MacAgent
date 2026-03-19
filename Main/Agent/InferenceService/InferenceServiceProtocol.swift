//
//  InferenceServiceProtocol.swift
//  InferenceService
//
//  Created by 张峰 on 2026/3/19.
//

import Foundation

@objc protocol InferenceXPCServiceProtocol {
    func ping(_ reply: @escaping (NSString?) -> Void)
    func generateReply(_ requestJSON: NSString, reply: @escaping (NSString?, NSString?) -> Void)
}
