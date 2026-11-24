import QtQuick
import QtQuick.Layouts
import QtQuick.Controls as QQC2

import org.kde.kirigami as Kirigami
import org.kde.kcmutils as KCM
import org.kde.kquickcontrols as KQC
import org.kde.plasma.plasma5support as Plasma5Support

KCM.SimpleKCM {
    id: page

    property alias cfg_binaryPath: binaryField.text
    property alias cfg_pollInterval: pollSpin.value
    property alias cfg_activityCheckInterval: activitySpin.value
    property string cfg_layoutMode
    property string cfg_gaugeLabel
    property alias cfg_showHeadings: headingsCheck.checked
    property alias cfg_warningThreshold: warningSpin.value
    property alias cfg_criticalThreshold: criticalSpin.value
    property alias cfg_currencySymbol: currencyField.text
    property alias cfg_backgroundOpacity: opacitySpin.value
    property string cfg_colorClaude
    property string cfg_colorCodex
    property string cfg_colorCursor
    property string cfg_colorOpenrouter
    property string cfg_colorTypesafe
    property string cfg_warningColor
    property string cfg_criticalColor

    // Defaults injected by the Plasma config system.
    property var cfg_binaryPathDefault
    property var cfg_pollIntervalDefault
    property var cfg_activityCheckIntervalDefault
    property var cfg_layoutModeDefault
    property var cfg_gaugeLabelDefault
    property var cfg_showHeadingsDefault
    property var cfg_warningThresholdDefault
    property var cfg_criticalThresholdDefault
    property var cfg_currencySymbolDefault
    property var cfg_backgroundOpacityDefault
    property var cfg_colorClaudeDefault
    property var cfg_colorCodexDefault
    property var cfg_colorCursorDefault
    property var cfg_colorOpenrouterDefault
    property var cfg_colorTypesafeDefault
    property var cfg_warningColorDefault
    property var cfg_criticalColorDefault

    readonly property var layoutModel: [
        { value: "grid", label: "Grid (wraps to fit)" },
        { value: "row", label: "Single row" },
        { value: "column", label: "One provider per line" }
    ]
    readonly property var labelModel: [
        { value: "percent", label: "Percentage" },
        { value: "time", label: "Time remaining" }
    ]

    function indexOf(model, value) {
        for (var i = 0; i < model.length; i++)
            if (model[i].value === value) return i
        return 0
    }

    Plasma5Support.DataSource {
        id: launcher
        engine: "executable"
        connectedSources: []
        onNewData: source => disconnectSource(source)
    }

    Kirigami.FormLayout {
        Kirigami.Separator { Kirigami.FormData.isSection: true; Kirigami.FormData.label: "Rings" }

        QQC2.ComboBox {
            Kirigami.FormData.label: "Layout:"
            model: page.layoutModel
            textRole: "label"
            currentIndex: page.indexOf(page.layoutModel, cfg_layoutMode)
            onActivated: cfg_layoutMode = page.layoutModel[currentIndex].value
        }
        QQC2.ComboBox {
            Kirigami.FormData.label: "Value inside quota rings:"
            model: page.labelModel
            textRole: "label"
            currentIndex: page.indexOf(page.labelModel, cfg_gaugeLabel)
            onActivated: cfg_gaugeLabel = page.labelModel[currentIndex].value
        }
        QQC2.CheckBox { id: headingsCheck; Kirigami.FormData.label: "Provider headings:"; text: "Show above each group" }

        Kirigami.Separator { Kirigami.FormData.isSection: true; Kirigami.FormData.label: "Thresholds" }

        QQC2.SpinBox { id: warningSpin; Kirigami.FormData.label: "Warning threshold (%):"; from: 10; to: criticalSpin.value; stepSize: 5 }
        QQC2.SpinBox { id: criticalSpin; Kirigami.FormData.label: "Critical threshold (%):"; from: warningSpin.value; to: 100; stepSize: 5 }

        Kirigami.Separator { Kirigami.FormData.isSection: true; Kirigami.FormData.label: "Colours" }

        KQC.ColorButton {
            Kirigami.FormData.label: "Claude:"
            color: cfg_colorClaude
            onColorChanged: if (String(color) !== cfg_colorClaude) cfg_colorClaude = String(color)
        }
        KQC.ColorButton {
            Kirigami.FormData.label: "Codex:"
            color: cfg_colorCodex
            onColorChanged: if (String(color) !== cfg_colorCodex) cfg_colorCodex = String(color)
        }
        KQC.ColorButton {
            Kirigami.FormData.label: "Cursor:"
            color: cfg_colorCursor
            onColorChanged: if (String(color) !== cfg_colorCursor) cfg_colorCursor = String(color)
        }
        KQC.ColorButton {
            Kirigami.FormData.label: "OpenRouter:"
            color: cfg_colorOpenrouter
            onColorChanged: if (String(color) !== cfg_colorOpenrouter) cfg_colorOpenrouter = String(color)
        }
        KQC.ColorButton {
            Kirigami.FormData.label: "JEV:"
            color: cfg_colorTypesafe
            onColorChanged: if (String(color) !== cfg_colorTypesafe) cfg_colorTypesafe = String(color)
        }
        KQC.ColorButton {
            Kirigami.FormData.label: "Warning:"
            color: cfg_warningColor
            onColorChanged: if (String(color) !== cfg_warningColor) cfg_warningColor = String(color)
        }
        KQC.ColorButton {
            Kirigami.FormData.label: "Critical:"
            color: cfg_criticalColor
            onColorChanged: if (String(color) !== cfg_criticalColor) cfg_criticalColor = String(color)
        }

        Kirigami.Separator { Kirigami.FormData.isSection: true; Kirigami.FormData.label: "Updates" }

        QQC2.SpinBox { id: pollSpin; Kirigami.FormData.label: "Poll interval (seconds):"; from: 60; to: 3600; stepSize: 30 }
        QQC2.SpinBox { id: activitySpin; Kirigami.FormData.label: "Activity check interval (seconds):"; from: 5; to: 120; stepSize: 5 }

        Kirigami.Separator { Kirigami.FormData.isSection: true; Kirigami.FormData.label: "Appearance" }

        QQC2.TextField { id: currencyField; Kirigami.FormData.label: "Currency symbol:"; maximumLength: 4 }
        QQC2.SpinBox { id: opacitySpin; Kirigami.FormData.label: "Popup background opacity (%):"; from: 0; to: 100; stepSize: 5 }

        Kirigami.Separator { Kirigami.FormData.isSection: true; Kirigami.FormData.label: "Providers" }

        QQC2.TextField { id: binaryField; Kirigami.FormData.label: "bingofuel command:"; placeholderText: "bingofuel" }
        QQC2.Button {
            Kirigami.FormData.label: "Providers, keys and rings:"
            icon.name: "utilities-terminal"
            text: "Run setup in a terminal…"
            onClicked: {
                // Same quoting as main.qml: only a leading ~/ is expanded.
                var q = s => "'" + String(s).replace(/'/g, "'\\''") + "'"
                var bin = (binaryField.text || "bingofuel").trim()
                var cmd = (bin.startsWith("~/") ? "\"$HOME\"/" + q(bin.slice(2)) : q(bin)) + " setup"
                // --hold keeps the window open so the summary and any error stay readable.
                launcher.connectSource("PATH=\"$HOME/.local/bin:$HOME/go/bin:/usr/local/bin:/usr/bin:/bin:$PATH\"; export PATH; "
                    + "if command -v konsole >/dev/null; then konsole --hold -e sh -c " + q(cmd) + "; "
                    + "elif command -v xdg-terminal-exec >/dev/null; then xdg-terminal-exec sh -c " + q(cmd + "; printf '\\nPress Enter to close'; read x") + "; "
                    + "else xterm -hold -e sh -c " + q(cmd) + "; fi")
            }
        }
        QQC2.Label {
            Layout.maximumWidth: Kirigami.Units.gridUnit * 22
            wrapMode: Text.Wrap
            font: Kirigami.Theme.smallFont
            text: "Which providers and rings appear, and the API keys, are set by `bingofuel setup`. Check problems with `bingofuel doctor`."
        }
    }
}
