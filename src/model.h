#pragma once

#include <QByteArray>
#include <QList>
#include <QString>
#include <QStringList>

namespace tailui {

enum class State { Connected, Disconnected, Connecting, NoWifi, Stopped, NeedsLogin, Error };
struct Status {
    State state = State::Error;
    QString name, address, authUrl, error, loginContext;
    bool hasLogin = false, requiresSignIn = false;
    QStringList health;
};

constexpr int kResponseLimit = 1024 * 1024;
bool decodeResponse(const QByteArray &wire, QByteArray *body, QString *error);
Status parseStatus(const QByteArray &json);
QString stateLabel(State state);
bool wifiAvailable(const QByteArray &routes, const QByteArray &wireless = QByteArray());

} // namespace tailui
