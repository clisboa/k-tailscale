#pragma once

#include "model.h"
#include <QObject>
#include <QLocalSocket>
#include <QTimer>
#include <functional>

namespace tailui {

class Client : public QObject {
public:
    explicit Client(const QString &socketPath = "/var/run/tailscale/tailscaled.sock", QObject *parent = nullptr, int timeoutMs = 3000);
    ~Client() override;
    bool refresh();
    void cancel();
    bool busy() const { return socket_ != nullptr; }
    std::function<void(const Status &)> completed;

private:
    void readAvailable(QLocalSocket *socket);
    void finish(const Status &status);
    void fail(const QString &message, State state = State::Error);
    QString path_;
    int timeoutMs_;
    QLocalSocket *socket_ = nullptr;
    QTimer timeout_;
    QByteArray wire_;
};

} // namespace tailui
