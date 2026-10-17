import QtQuick 2.15

Rectangle {
    id: root
    width: 640
    height: 480
    color: "steelblue"

    property int count: 0
    signal clicked()

    Text {
        anchors.centerIn: parent
        text: "Hello, QML"
    }

    MouseArea {
        anchors.fill: parent
        onClicked: root.clicked()
    }
}
