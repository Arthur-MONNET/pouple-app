import AppKit
import PoupleCore
import SwiftUI
import WebKit

/// The main window: the core's web UI in a WKWebView, with a native placeholder for every moment there is no page.
@MainActor
final class MainWindowController: NSObject, NSWindowDelegate, WKNavigationDelegate, WKUIDelegate {
    private let window: NSWindow
    private let webView: WKWebView
    private let placeholder = PlaceholderModel()
    private let placeholderHost: NSHostingView<PlaceholderView>
    private var loadedURL: URL?
    private var policy: NavigationPolicy?
    private var activeNavigation: WKNavigation?
    private var pageShown = false
    private var coreState: CoreState = .starting

    /// Called when the window closes, so the app can leave the Dock.
    var onClose: () -> Void = {}
    var onRetry: () -> Void = {}

    override init() {
        let configuration = WKWebViewConfiguration()
        // Nothing the page stores survives the launch: its token and port change every time anyway.
        configuration.websiteDataStore = .nonPersistent()
        webView = WKWebView(frame: .zero, configuration: configuration)
        webView.underPageBackgroundColor = WindowBackground.nsColor
        webView.allowsBackForwardNavigationGestures = false
        webView.isInspectable = Self.inspectorEnabled

        window = NSWindow(
            contentRect: NSRect(x: 0, y: 0, width: 1100, height: 760),
            styleMask: [.titled, .closable, .miniaturizable, .resizable, .fullSizeContentView],
            backing: .buffered,
            defer: false
        )
        placeholderHost = NSHostingView(rootView: PlaceholderView(model: placeholder))
        super.init()
        placeholder.retry = { [weak self] in self?.retry() }

        window.title = L10n.text(.windowTitle)
        window.titleVisibility = .hidden
        window.titlebarAppearsTransparent = true
        window.appearance = NSAppearance(named: .darkAqua)
        window.backgroundColor = WindowBackground.nsColor
        window.isReleasedWhenClosed = false
        window.contentMinSize = NSSize(width: 760, height: 560)
        window.delegate = self

        let container = NSView()
        container.wantsLayer = true
        container.layer?.backgroundColor = WindowBackground.nsColor.cgColor
        window.contentView = container
        for view in [webView, placeholderHost] {
            view.translatesAutoresizingMaskIntoConstraints = false
            container.addSubview(view)
        }
        // The page starts under the title bar, so the traffic lights never cover the web UI.
        let content = window.contentLayoutGuide as! NSLayoutGuide
        NSLayoutConstraint.activate([
            webView.topAnchor.constraint(equalTo: content.topAnchor),
            webView.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            webView.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            webView.bottomAnchor.constraint(equalTo: container.bottomAnchor),
            placeholderHost.topAnchor.constraint(equalTo: container.topAnchor),
            placeholderHost.leadingAnchor.constraint(equalTo: container.leadingAnchor),
            placeholderHost.trailingAnchor.constraint(equalTo: container.trailingAnchor),
            placeholderHost.bottomAnchor.constraint(equalTo: container.bottomAnchor),
        ])
        webView.navigationDelegate = self
        webView.uiDelegate = self
        webView.isHidden = true

        // A saved frame wins; a first launch is centered.
        if !window.setFrameAutosaveName("PoupleMainWindow") { window.center() }
    }

    /// The Web Inspector is a debugging aid: always off in a release build, opt-in with `POUPLE_WEB_INSPECTOR=1` in a debug one.
    static var inspectorEnabled: Bool {
        #if DEBUG
        return ProcessInfo.processInfo.environment["POUPLE_WEB_INSPECTOR"] == "1"
        #else
        return false
        #endif
    }

    var isVisible: Bool { window.isVisible }

    /// The language changed: what AppKit holds as text follows (SwiftUI views redraw on their own).
    func languageChanged() {
        window.title = L10n.text(.windowTitle)
    }

    func show() {
        NSApp.setActivationPolicy(.regular)
        window.makeKeyAndOrderFront(nil)
        NSApp.activate(ignoringOtherApps: true)
    }

    func reload() {
        if pageShown { activeNavigation = webView.reload() }
    }

    func refreshWebLanguage() {
        guard loadedURL != nil else { return }
        webView.evaluateJavaScript("window.dispatchEvent(new Event('pouple:language-changed'))", completionHandler: nil)
    }

    // MARK: - Core state

    func coreChanged(_ state: CoreState, token: String?) {
        coreState = state
        switch state {
        case .starting:
            clearPage()
            showPlaceholder(.starting)
        case let .failed(failure):
            clearPage()
            showPlaceholder(.failed(failure))
        case let .ready(port):
            let url = URL(string: "http://127.0.0.1:\(port)/")!
            guard let token else { return }
            policy = NavigationPolicy(port: port)
            if loadedURL != url {
                loadedURL = url
                showPlaceholder(.loading)
                load(url, port: port, token: token)
            } else if !pageShown {
                load(url, port: port, token: token)
            }
        }
    }

    private func clearPage() {
        loadedURL = nil
        policy = nil
        activeNavigation = nil
        webView.stopLoading()
        webView.configuration.userContentController.removeAllUserScripts()
    }

    /// Defines `window.__POUPLE__` before the page runs, for the core's own origin only.
    private func load(_ url: URL, port: Int, token: String) {
        let controller = webView.configuration.userContentController
        controller.removeAllUserScripts()
        controller.addUserScript(WKUserScript(
            source: PageBootstrap.userScript(token: token, port: port),
            injectionTime: .atDocumentStart,
            forMainFrameOnly: true
        ))
        activeNavigation = webView.load(URLRequest(url: url))
    }

    private func showPlaceholder(_ mode: PlaceholderModel.Mode) {
        pageShown = false
        webView.isHidden = true
        placeholderHost.isHidden = false
        placeholder.mode = mode
    }

    private func showPage() {
        pageShown = true
        webView.isHidden = false
        placeholderHost.isHidden = true
    }

    private func retry() {
        if case .failed = coreState {
            placeholder.mode = .starting
            onRetry()
        } else if let loadedURL {
            placeholder.mode = .loading
            activeNavigation = webView.load(URLRequest(url: loadedURL))
        }
    }

    // MARK: - NSWindowDelegate

    func windowWillClose(_ notification: Notification) {
        onClose()
    }

    // MARK: - WKNavigationDelegate

    func webView(_ webView: WKWebView, decidePolicyFor navigationAction: WKNavigationAction) async -> WKNavigationActionPolicy {
        let url = navigationAction.request.url
        switch policy?.decision(for: url) ?? .deny {
        case .allow:
            return .allow
        case .openExternally:
            // A frame inside the page must not open a browser by itself: only a link or a new window does.
            if navigationAction.targetFrame?.isMainFrame ?? true, let url { NSWorkspace.shared.open(url) }
            return .cancel
        case .deny:
            return .cancel
        }
    }

    func webView(_ webView: WKWebView, didFinish navigation: WKNavigation!) {
        guard let navigation, navigation === activeNavigation else { return }
        showPage()
        #if DEBUG
        // Development aid: `POUPLE_DEBUG_JS=<expression>` prints what it evaluates to once the page has loaded.
        if let expression = ProcessInfo.processInfo.environment["POUPLE_DEBUG_JS"] {
            webView.evaluateJavaScript(expression) { result, error in
                print("POUPLE_DEBUG_JS ->", result.map { "\($0)" } ?? "nil", error?.localizedDescription ?? ""); fflush(stdout)
            }
        }
        #endif
    }

    func webView(_ webView: WKWebView, didFailProvisionalNavigation navigation: WKNavigation!, withError error: Error) {
        guard let navigation, navigation === activeNavigation else { return }
        showPlaceholder(.failed(.other(error.localizedDescription)))
    }

    func webView(_ webView: WKWebView, didFail navigation: WKNavigation!, withError error: Error) {
        guard let navigation, navigation === activeNavigation else { return }
        showPlaceholder(.failed(.other(error.localizedDescription)))
    }

    func webViewWebContentProcessDidTerminate(_ webView: WKWebView) {
        guard let loadedURL else { return }
        showPlaceholder(.loading)
        activeNavigation = webView.load(URLRequest(url: loadedURL))
    }

    // MARK: - WKUIDelegate

    /// `target="_blank"` and `window.open`: never a second web view, the default browser instead.
    func webView(
        _ webView: WKWebView,
        createWebViewWith configuration: WKWebViewConfiguration,
        for navigationAction: WKNavigationAction,
        windowFeatures: WKWindowFeatures
    ) -> WKWebView? {
        if let url = navigationAction.request.url, policy?.decision(for: url) == .openExternally {
            NSWorkspace.shared.open(url)
        }
        return nil
    }

    func webView(_ webView: WKWebView, runJavaScriptAlertPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo) async {
        let alert = NSAlert()
        alert.messageText = message
        alert.runModal()
    }

    func webView(_ webView: WKWebView, runJavaScriptConfirmPanelWithMessage message: String, initiatedByFrame frame: WKFrameInfo) async -> Bool {
        let alert = NSAlert()
        alert.messageText = message
        alert.addButton(withTitle: L10n.text(.dialogOK))
        alert.addButton(withTitle: L10n.text(.dialogCancel))
        return alert.runModal() == .alertFirstButtonReturn
    }

    func webView(_ webView: WKWebView, runOpenPanelWith parameters: WKOpenPanelParameters, initiatedByFrame frame: WKFrameInfo) async -> [URL]? {
        let panel = NSOpenPanel()
        panel.allowsMultipleSelection = parameters.allowsMultipleSelection
        panel.canChooseDirectories = parameters.allowsDirectories
        return panel.runModal() == .OK ? panel.urls : nil
    }
}
