#include "model.h"

#include <QHostAddress>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <QRegExp>
#include <QSet>
#include <QUrl>

namespace tailui {

bool decodeResponse(const QByteArray &wire, QByteArray *body, QString *error) {
    if (!body || !error) return false;
    body->clear(); error->clear();
    auto fail = [error](const QString &message) { *error = message; return false; };
    if (wire.size() > kResponseLimit) return fail("Tailscale response exceeded the size limit.");
    const int end = wire.indexOf("\r\n\r\n");
    if (end < 0 || end > 16384) return fail("Incomplete or oversized Tailscale response headers.");
    const QList<QByteArray> lines = wire.left(end).split('\n');
    QRegExp status("^HTTP/1\\.[01] ([0-9]{3})( .*)?$");
    if (lines.isEmpty() || !status.exactMatch(QString::fromLatin1(lines.first().trimmed())))
        return fail("Invalid Tailscale HTTP response.");
    const int code = status.cap(1).toInt();
    if (code != 200) return fail(QString("Tailscale returned HTTP %1.").arg(code));
    qint64 length = -1;
    for (int i = 1; i < lines.size(); ++i) {
        const QByteArray line = lines[i].trimmed();
        const int colon = line.indexOf(':');
        if (colon <= 0) return fail("Invalid Tailscale response header.");
        const QByteArray key = line.left(colon).trimmed().toLower();
        const QByteArray value = line.mid(colon + 1).trimmed();
        if (key == "transfer-encoding" || (key == "content-encoding" && value != "identity"))
            return fail("Unexpected Tailscale response encoding.");
        if (key == "content-length") {
            bool ok = false;
            const qint64 candidate = value.toLongLong(&ok);
            if (length >= 0 || !ok || !QRegExp("[0-9]+").exactMatch(QString::fromLatin1(value)) || candidate > kResponseLimit)
                return fail("Invalid Tailscale content length.");
            length = candidate;
        }
    }
    const QByteArray payload = wire.mid(end + 4);
    if (length >= 0 && payload.size() != length) return fail("Incomplete Tailscale response body.");
    *body = payload;
    return true;
}

namespace {
QString ipv4(const QJsonValue &value) {
    for (const QJsonValue &item : value.toArray()) {
        if (!item.isString()) continue;
        const QHostAddress address(item.toString());
        if (address.protocol() == QAbstractSocket::IPv4Protocol) return address.toString();
    }
    return QString();
}
}

Status parseStatus(const QByteArray &json) {
    Status out;
    if (json.size() > kResponseLimit) {
        out.error = "Tailscale status exceeded the size limit."; return out;
    }
    QJsonParseError error;
    const QJsonDocument document = QJsonDocument::fromJson(json, &error);
    if (error.error != QJsonParseError::NoError || !document.isObject()) {
        out.error = "Invalid Tailscale status data."; return out;
    }
    const QJsonObject root = document.object();
    const QString backend = root.value("BackendState").toString();
    if (backend == "Running") out.state = State::Connected;
    else if (backend == "Stopped") out.state = State::Disconnected;
    else if (backend == "Starting" || backend == "NoState") out.state = State::Connecting;
    else if (backend == "NeedsLogin" || backend == "NeedsMachineAuth") out.state = State::NeedsLogin;
    else { out.error = "Tailscale reported an unrecognized connection state."; return out; }
    if (backend == "NeedsMachineAuth") out.health << "Approve this device in the Tailscale admin console.";
    const QJsonObject self = root.value("Self").toObject();
    out.requiresSignIn = backend == "NeedsLogin";
    out.hasLogin = root.value("HaveNodeKey").toBool();
    out.loginContext = self.value("ID").toString()+"\n"+root.value("CurrentTailnet").toObject().value("StableID").toString();
    out.name = self.value("HostName").toString().left(128);
    out.address = ipv4(self.value("TailscaleIPs"));
    const QUrl auth(root.value("AuthURL").toString());
    if (auth.isValid() && !auth.host().isEmpty() && (auth.scheme() == "https" || auth.scheme() == "http"))
        out.authUrl = auth.toString();
    for (const QJsonValue &item : root.value("Health").toArray()) {
        if (item.isString() && out.health.size() < 16) out.health << item.toString().left(256);
    }
    return out;
}

QString stateLabel(State state) {
    switch (state) {
    case State::Connected: return "Connected";
    case State::Disconnected: return "Disconnected";
    case State::Connecting: return "Connecting";
    case State::NoWifi: return "Wi-Fi unavailable";
    case State::Stopped: return "Service stopped";
    case State::NeedsLogin: return "Sign-in required";
    case State::Error: return "Status unavailable";
    }
    return "Status unavailable";
}

bool wifiAvailable(const QByteArray &routes, const QByteArray &wireless) {
    QSet<QByteArray> interfaces;
    for (const QByteArray &line : wireless.split('\n')) {
        const QList<QByteArray> fields=line.simplified().split(' ');
        if(!fields.isEmpty()&&fields[0].endsWith(":"))interfaces.insert(fields[0].left(fields[0].size()-1));
    }
    for (const QByteArray &line : routes.split('\n')) {
        const QList<QByteArray> fields = line.simplified().split(' ');
        if (fields.size() < 4 || (!fields[0].startsWith("wlan") && !interfaces.contains(fields[0])) || fields[1] != "00000000" || fields[2] == "00000000") continue;
        bool ok = false; const uint flags = fields[3].toUInt(&ok, 16);
        if (ok && (flags & 3) == 3) return true;
    }
    return false;
}

} // namespace tailui
