// Renders the desktop rings outside Plasma and saves a PNG.
// Usage: QML_XHR_ALLOW_FILE_READ=1 qml preview.qml -- <fetch.json> <out.png> [width] [height] [layout] [font family]
import QtQuick
import QtQuick.Window
import org.kde.kirigami as Kirigami

import "../package/contents/ui"

Window {
    id: win
    readonly property var args: Qt.application.arguments.slice(Qt.application.arguments.indexOf("--") + 1)
    width: Number(args[2] || 560)
    height: Number(args[3] || 430)
    visible: true
    color: "#0b1220"

    Store {
        id: store
        providerColors: ({ claude: "#D77757", codex: "#FFFFFF", cursor: "#3CCFB4", openrouter: "#7AB8FF", typesafe: "#D45BB6" })
        textColor: "#eeeeee"
    }

    Rectangle { anchors.fill: parent; color: win.color }

    Rings {
        id: rings
        anchors.fill: parent
        anchors.margins: 16
        // Mimic a dark Plasma theme offscreen.
        Kirigami.Theme.inherit: false
        Kirigami.Theme.backgroundColor: "#0b1220"
        Kirigami.Theme.textColor: "#eeeeee"
        Kirigami.Theme.disabledTextColor: "#9aa0a6"
        store: store
        layoutMode: win.args[4] || "grid"
        fontFamily: win.args[5] || Qt.application.font.family
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
        interval: 400
        onTriggered: win.contentItem.grabToImage(r => { r.saveToFile(win.args[1]); Qt.quit() })
    }
}
