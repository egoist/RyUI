#if canImport(UIKit) && ASUKA_COMPOSE // an app that calls asuka/sdk/compose (see the CLI's iosFeatures)
import UIKit
import MessageUI

/// asuka/sdk/compose's module: "mail" (JSON: to, cc, bcc, subject, body, html,
/// text, mailto, attachments: source, type, name) shows Mail's composer,
/// or opens the mailto: link when the device has no Mail account and
/// there are no attachments; "message" (to, subject, body, attachments)
/// shows Messages' composer. Both answer u8: 0 cancelled, 1 sent, 2
/// saved, 3 unknown (the mailto: link); "unavailable" if they can't.
/// "can" answers u8s: mail, Mail's composer, texts, attachments.
@MainActor final class AsukaCompose: NSObject, MFMailComposeViewControllerDelegate, MFMessageComposeViewControllerDelegate {
    static let shared = AsukaCompose()
    private var replies: [ObjectIdentifier: (Result<Data, Error>) -> Void] = [:]

    func methods(_ rt: AsukaRuntime) -> [String: AsukaModuleMethod] {
        [
            "can": { _, reply in
                let composer = MFMailComposeViewController.canSendMail()
                let mailto = URL(string: "mailto:").map { UIApplication.shared.canOpenURL($0) } ?? false
                reply(.success(Data([
                    composer || mailto ? 1 : 0, composer ? 1 : 0,
                    MFMessageComposeViewController.canSendText() ? 1 : 0,
                    MFMessageComposeViewController.canSendAttachments() ? 1 : 0,
                ])))
            },
            "mail": { [unowned self, weak rt] args, reply in
                let o = Self.json(args)
                let files = Self.attachments(o)
                guard MFMailComposeViewController.canSendMail() else {
                    // Another mail app the user set, through its link: no attachments.
                    guard files.isEmpty, let url = URL(string: o["mailto"] as? String ?? "") else {
                        return reply(.failure(AsukaModuleError("unavailable")))
                    }
                    UIApplication.shared.open(url) { ok in reply(ok ? .success(Data([3])) : .failure(AsukaModuleError("unavailable"))) }
                    return
                }
                AsukaLocalFile.resolve(files.map { ($0.source, $0.type) }) { r in
                    switch r {
                    case .failure(let e): reply(.failure(e))
                    case .success(let urls):
                        guard let top = rt?.topController else { return reply(.failure(AsukaModuleError("no screen"))) }
                        let vc = MFMailComposeViewController()
                        vc.mailComposeDelegate = self
                        vc.setToRecipients(o["to"] as? [String] ?? [])
                        vc.setCcRecipients(o["cc"] as? [String] ?? [])
                        vc.setBccRecipients(o["bcc"] as? [String] ?? [])
                        vc.setSubject(o["subject"] as? String ?? "")
                        vc.setMessageBody(o["body"] as? String ?? "", isHTML: o["html"] as? Bool ?? false)
                        for (u, f) in zip(urls, files) {
                            guard let d = try? Data(contentsOf: u) else { return reply(.failure(AsukaModuleError("couldn't read \(u.path)"))) }
                            vc.addAttachmentData(d, mimeType: f.type, fileName: f.name)
                        }
                        self.replies[ObjectIdentifier(vc)] = reply
                        top.present(vc, animated: true)
                    }
                }
            },
            "message": { [unowned self, weak rt] args, reply in
                let o = Self.json(args)
                let files = Self.attachments(o)
                guard MFMessageComposeViewController.canSendText(),
                      files.isEmpty || MFMessageComposeViewController.canSendAttachments() else {
                    return reply(.failure(AsukaModuleError("unavailable")))
                }
                AsukaLocalFile.resolve(files.map { ($0.source, $0.type) }) { r in
                    switch r {
                    case .failure(let e): reply(.failure(e))
                    case .success(let urls):
                        guard let top = rt?.topController else { return reply(.failure(AsukaModuleError("no screen"))) }
                        let vc = MFMessageComposeViewController()
                        vc.messageComposeDelegate = self
                        vc.recipients = o["to"] as? [String] ?? []
                        var body = o["body"] as? String ?? ""
                        let subject = o["subject"] as? String ?? ""
                        if !subject.isEmpty {
                            // A subject where the user turned them on; else before the body.
                            if MFMessageComposeViewController.canSendSubject() { vc.subject = subject }
                            else { body = body.isEmpty ? subject : subject + "\n" + body }
                        }
                        vc.body = body
                        for (u, f) in zip(urls, files) {
                            guard vc.addAttachmentURL(u, withAlternateFilename: f.name) else {
                                return reply(.failure(AsukaModuleError("couldn't attach \(f.name)")))
                            }
                        }
                        self.replies[ObjectIdentifier(vc)] = reply
                        top.present(vc, animated: true)
                    }
                }
            },
        ]
    }

    private static func json(_ d: Data) -> [String: Any] {
        (try? JSONSerialization.jsonObject(with: d)) as? [String: Any] ?? [:]
    }

    private static func attachments(_ o: [String: Any]) -> [(source: String, type: String, name: String)] {
        (o["attachments"] as? [[String: Any]] ?? []).map {
            ($0["source"] as? String ?? "", $0["type"] as? String ?? "application/octet-stream", $0["name"] as? String ?? "file")
        }
    }

    private func finish(_ vc: UIViewController, _ r: Result<Data, Error>) {
        vc.dismiss(animated: true)
        replies.removeValue(forKey: ObjectIdentifier(vc))?(r)
    }

    nonisolated func mailComposeController(_ vc: MFMailComposeViewController, didFinishWith result: MFMailComposeResult, error: Error?) {
        MainActor.assumeIsolated {
            switch result {
            case .sent: finish(vc, .success(Data([1])))
            case .saved: finish(vc, .success(Data([2])))
            case .failed: finish(vc, .failure(error ?? AsukaModuleError("the mail couldn't be sent")))
            default: finish(vc, .success(Data([0])))
            }
        }
    }

    nonisolated func messageComposeViewController(_ vc: MFMessageComposeViewController, didFinishWith result: MessageComposeResult) {
        MainActor.assumeIsolated {
            switch result {
            case .sent: finish(vc, .success(Data([1])))
            case .failed: finish(vc, .failure(AsukaModuleError("the message couldn't be sent")))
            default: finish(vc, .success(Data([0])))
            }
        }
    }
}
#endif
