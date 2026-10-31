#if canImport(UIKit) && RYUI_CONTACTS // an app that calls ryui/contacts (see the CLI's iosFeatures)
import UIKit
import Contacts
import ContactsUI

/// ryui/contacts' module: "request" (u8 access: not asked, denied,
/// limited, full), "list" (JSON: name, limit) and "pick" answer contacts
/// as JSON; "add" (a contact's JSON) shows the new contact screen and
/// answers u8 saved.
@MainActor final class RyUIContacts: NSObject, CNContactPickerDelegate, CNContactViewControllerDelegate {
    static let shared = RyUIContacts()
    private let store = CNContactStore()
    private var picking: ((Result<Data, Error>) -> Void)?
    private var adding: ((Result<Data, Error>) -> Void)?

    static func access() -> UInt8 {
        switch CNContactStore.authorizationStatus(for: .contacts) {
        case .authorized: return 3
        case .denied, .restricted: return 1
        case .notDetermined: return 0
        default: return 2 // limited (iOS 18)
        }
    }

    static let keys: [CNKeyDescriptor] = [
        CNContactFormatter.descriptorForRequiredKeys(for: .fullName),
        CNContactGivenNameKey as CNKeyDescriptor, CNContactFamilyNameKey as CNKeyDescriptor,
        CNContactOrganizationNameKey as CNKeyDescriptor, CNContactJobTitleKey as CNKeyDescriptor,
        CNContactPhoneNumbersKey as CNKeyDescriptor, CNContactEmailAddressesKey as CNKeyDescriptor,
        CNContactBirthdayKey as CNKeyDescriptor,
    ]

    nonisolated static func json(_ c: CNContact) -> [String: Any] {
        func label(_ l: String?) -> String { l.map { CNLabeledValue<NSString>.localizedString(forLabel: $0) } ?? "" }
        var birthday = ""
        if c.isKeyAvailable(CNContactBirthdayKey), let b = c.birthday, let m = b.month, let d = b.day {
            birthday = (b.year.map { String(format: "%04d", $0) } ?? "-") + String(format: "-%02d-%02d", m, d)
        }
        return [
            "id": c.identifier,
            "name": CNContactFormatter.string(from: c, style: .fullName) ?? "",
            "givenName": c.isKeyAvailable(CNContactGivenNameKey) ? c.givenName : "",
            "familyName": c.isKeyAvailable(CNContactFamilyNameKey) ? c.familyName : "",
            "organization": c.isKeyAvailable(CNContactOrganizationNameKey) ? c.organizationName : "",
            "jobTitle": c.isKeyAvailable(CNContactJobTitleKey) ? c.jobTitle : "",
            "phones": c.isKeyAvailable(CNContactPhoneNumbersKey) ? c.phoneNumbers.map { ["label": label($0.label), "value": $0.value.stringValue] } : [],
            "emails": c.isKeyAvailable(CNContactEmailAddressesKey) ? c.emailAddresses.map { ["label": label($0.label), "value": $0.value as String] } : [],
            "birthday": birthday,
            "note": "", // the note needs an entitlement Apple grants on request
        ]
    }

    func methods(_ rt: RyUIRuntime) -> [String: RyUIModuleMethod] {
        [
            "request": { [unowned self] _, reply in
                guard CNContactStore.authorizationStatus(for: .contacts) == .notDetermined else { reply(.success(Data([Self.access()]))); return }
                self.store.requestAccess(for: .contacts) { _, _ in
                    DispatchQueue.main.async { reply(.success(Data([Self.access()]))) }
                }
            },
            "list": { [unowned self] args, reply in
                guard Self.access() >= 2 else { reply(.failure(RyUIModuleError("not allowed"))); return }
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                let name = o["name"] as? String ?? "", limit = o["limit"] as? Int ?? 0
                let store = self.store
                DispatchQueue.global(qos: .userInitiated).async {
                    let req = CNContactFetchRequest(keysToFetch: Self.keys)
                    req.sortOrder = .userDefault
                    if !name.isEmpty { req.predicate = CNContact.predicateForContacts(matchingName: name) }
                    var out: [[String: Any]] = []
                    do {
                        try store.enumerateContacts(with: req) { c, stop in
                            out.append(Self.json(c))
                            if limit > 0 && out.count >= limit { stop.pointee = true }
                        }
                        reply(.success(try JSONSerialization.data(withJSONObject: out)))
                    } catch {
                        reply(.failure(error))
                    }
                }
            },
            "pick": { [unowned self, weak rt] _, reply in
                guard let top = rt?.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
                let p = CNContactPickerViewController()
                p.delegate = self
                self.picking = reply
                top.present(p, animated: true)
            },
            "add": { [unowned self, weak rt] args, reply in
                guard let top = rt?.topController else { reply(.failure(RyUIModuleError("no screen"))); return }
                let o = (try? JSONSerialization.jsonObject(with: args)) as? [String: Any] ?? [:]
                let c = CNMutableContact()
                c.givenName = o["givenName"] as? String ?? ""
                c.familyName = o["familyName"] as? String ?? ""
                if c.givenName.isEmpty && c.familyName.isEmpty { c.givenName = o["name"] as? String ?? "" }
                c.organizationName = o["organization"] as? String ?? ""
                c.jobTitle = o["jobTitle"] as? String ?? ""
                let items = { (k: String) in (o[k] as? [[String: Any]] ?? []).map { ($0["label"] as? String ?? "", $0["value"] as? String ?? "") } }
                c.phoneNumbers = items("phones").map { CNLabeledValue(label: $0.0.isEmpty ? CNLabelPhoneNumberMobile : $0.0, value: CNPhoneNumber(stringValue: $0.1)) }
                c.emailAddresses = items("emails").map { CNLabeledValue(label: $0.0.isEmpty ? CNLabelHome : $0.0, value: $0.1 as NSString) }
                if let b = o["birthday"] as? String {
                    let p = b.split(separator: "-", omittingEmptySubsequences: false).map(String.init)
                    var d = DateComponents()
                    if p.count == 3 { d.year = Int(p[0]); d.month = Int(p[1]); d.day = Int(p[2]) }
                    else if p.count == 4 { d.month = Int(p[2]); d.day = Int(p[3]) } // --MM-DD
                    if d.month != nil && d.day != nil { c.birthday = d }
                }
                let vc = CNContactViewController(forNewContact: c)
                vc.contactStore = self.store
                vc.delegate = self
                self.adding = reply
                top.present(UINavigationController(rootViewController: vc), animated: true)
            },
        ]
    }

    nonisolated func contactPicker(_ picker: CNContactPickerViewController, didSelect contact: CNContact) {
        let d = (try? JSONSerialization.data(withJSONObject: Self.json(contact))) ?? Data()
        MainActor.assumeIsolated {
            picking?(.success(d))
            picking = nil
        }
    }

    nonisolated func contactPickerDidCancel(_ picker: CNContactPickerViewController) {
        MainActor.assumeIsolated {
            picking?(.success(Data()))
            picking = nil
        }
    }

    nonisolated func contactViewController(_ vc: CNContactViewController, didCompleteWith contact: CNContact?) {
        MainActor.assumeIsolated {
            vc.dismiss(animated: true)
            adding?(.success(Data([contact == nil ? 0 : 1])))
            adding = nil
        }
    }
}
#endif
