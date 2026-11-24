// Renders the popup outside Plasma and saves a PNG.
// Usage: QML_XHR_ALLOW_FILE_READ=1 qml preview-popup.qml -- <fetch.json> <out.png> [font family]
import QtQuick
import QtQuick.Controls as QQC2
import org.kde.kirigami as Kirigami

import "../package/contents/ui"

QQC2.ApplicationWindow {
    id: win
    readonly property var args: Qt.application.arguments.slice(Qt.application.arguments.indexOf("--") + 1)
    width: 440
    height: 540
    visible: true
    color: "#0b1220"
    // Offscreen there is no Plasma style; give the default style a dark palette.
    palette.window: "#141b2b"
    palette.windowText: "#eeeeee"
    palette.text: "#eeeeee"
    palette.base: "#0f1522"
    palette.button: "#232c40"
    palette.buttonText: "#eeeeee"
    palette.placeholderText: "#7d8590"
    palette.mid: "#3a4256"
    font.family: args[2] || Qt.application.font.family

    Rectangle { anchors.fill: parent; color: win.color }

    Store {
        id: store
        providerColors: ({ claude: "#D77757", codex: "#FFFFFF", cursor: "#3CCFB4", openrouter: "#7AB8FF", typesafe: "#D45BB6" })
        textColor: "#eeeeee"
    }

    Details {
        id: details
        anchors.fill: parent
        anchors.margins: 8
        store: store
        backgroundAlpha: 0.9
        Kirigami.Theme.inherit: false
        Kirigami.Theme.colorSet: Kirigami.Theme.Window
        Kirigami.Theme.backgroundColor: "#141b2b"
        Kirigami.Theme.textColor: "#eeeeee"
        Kirigami.Theme.disabledTextColor: "#9aa0a6"
    }

    Component.onCompleted: {
        var xhr = new XMLHttpRequest()
        xhr.open("GET", "file://" + args[0], false)
        xhr.send()
        store.parse(xhr.responseText)
        shot.start()
    }

    Timer {
        id: shot
        interval: 500
        onTriggered: { var ok = details.grabToImage(r => { r.saveToFile(win.args[1]); Qt.quit() }); if (!ok) { console.log("grab failed"); Qt.quit() } }
    }
}
