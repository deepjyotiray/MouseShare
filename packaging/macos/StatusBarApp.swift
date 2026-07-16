import AppKit
import Foundation

final class MouseShareDelegate: NSObject, NSApplicationDelegate {
    private var statusItem: NSStatusItem!
    private var backendProcess: Process?
    private var urlFilePath: String = ""
    private var logFilePath: String = ""

    func applicationDidFinishLaunching(_ notification: Notification) {
        NSApp.setActivationPolicy(.accessory)
        configurePaths()
        launchBackend()
        buildStatusItem()
    }

    func applicationWillTerminate(_ notification: Notification) {
        backendProcess?.terminate()
    }

    private func configurePaths() {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first!
        let baseDir = appSupport.appendingPathComponent("MouseShare", isDirectory: true)
        try? FileManager.default.createDirectory(at: baseDir, withIntermediateDirectories: true)
        urlFilePath = baseDir.appendingPathComponent("ui-url.txt").path
        logFilePath = baseDir.appendingPathComponent("mouseshare.log").path
    }

    private func launchBackend() {
        guard let resourcePath = Bundle.main.resourcePath else { return }
        let backendPath = resourcePath + "/MouseShareBackend"
        guard FileManager.default.fileExists(atPath: backendPath) else { return }

        let process = Process()
        process.executableURL = URL(fileURLWithPath: backendPath)
        var environment = ProcessInfo.processInfo.environment
        environment.merge([
            "MOUSESHARE_NO_AUTO_OPEN": "1",
            "MOUSESHARE_UI_URL_FILE": urlFilePath,
        ]) { _, new in new }
        process.environment = environment
        process.standardOutput = Pipe()
        process.standardError = Pipe()
        do {
            try process.run()
            backendProcess = process
        } catch {
            NSLog("MouseShare backend launch failed: \(error.localizedDescription)")
        }
    }

    private func buildStatusItem() {
        statusItem = NSStatusBar.system.statusItem(withLength: NSStatusItem.variableLength)
        if let button = statusItem.button {
            button.image = NSImage(contentsOf: Bundle.main.url(forResource: "mouseshare", withExtension: "icns")!)
            button.image?.size = NSSize(width: 14, height: 14)
            button.image?.isTemplate = true
            button.imagePosition = .imageOnly
            button.title = ""
            button.toolTip = "MouseShare"
        }

        let menu = NSMenu()
        menu.addItem(withTitle: "Open MouseShare", action: #selector(openDashboard), keyEquivalent: "")
        menu.addItem(withTitle: "Open Logs", action: #selector(openLogs), keyEquivalent: "")
        menu.addItem(.separator())
        menu.addItem(withTitle: "Quit MouseShare", action: #selector(quitApp), keyEquivalent: "q")
        menu.items.forEach { $0.target = self }
        statusItem.menu = menu
    }

    @objc private func openDashboard() {
        guard let url = currentDashboardURL() else { return }
        NSWorkspace.shared.open(url)
    }

    @objc private func openLogs() {
        NSWorkspace.shared.open(URL(fileURLWithPath: logFilePath))
    }

    @objc private func quitApp() {
        NSApp.terminate(nil)
    }

    private func currentDashboardURL() -> URL? {
        guard let data = try? Data(contentsOf: URL(fileURLWithPath: urlFilePath)),
              let raw = String(data: data, encoding: .utf8)?
                .trimmingCharacters(in: .whitespacesAndNewlines),
              !raw.isEmpty else {
            return nil
        }
        return URL(string: raw)
    }
}

let app = NSApplication.shared
let delegate = MouseShareDelegate()
app.delegate = delegate
app.run()
