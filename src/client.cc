#include "client.h"

namespace tailui {

Client::Client(const QString &socketPath, QObject *parent, int timeoutMs)
    : QObject(parent), path_(socketPath), timeoutMs_(timeoutMs) {
    timeout_.setSingleShot(true);
    QObject::connect(&timeout_, &QTimer::timeout, this, [this] { fail("Tailscale status request timed out. Status will refresh automatically."); });
}

Client::~Client() {
    completed = nullptr; cancel();
}

void Client::cancel() {
    QLocalSocket *socket = socket_;
    socket_ = nullptr; timeout_.stop(); wire_.clear();
    if (socket) { socket->abort(); socket->deleteLater(); }
}

bool Client::refresh() {
    if (socket_) return false;
    wire_.clear();
    QLocalSocket *socket = new QLocalSocket(this);
    socket_ = socket;
    QObject::connect(socket, &QLocalSocket::connected, this, [this, socket] {
        if (socket_ != socket) return;
        const QByteArray path = "/localapi/v0/status?peers=false";
        socket->write("GET " + path + " HTTP/1.0\r\nHost: local-tailscaled.sock\r\nConnection: close\r\n\r\n");
    });
    QObject::connect(socket, &QLocalSocket::readyRead, this, [this, socket] { readAvailable(socket); });
    QObject::connect(socket, &QLocalSocket::disconnected, this, [this, socket] {
        if (socket_ != socket) return;
        readAvailable(socket);
        if (socket_ != socket) return;
        QByteArray body; QString error;
        if (!decodeResponse(wire_, &body, &error)) fail(error);
        else finish(parseStatus(body));
    });
    auto error = [this, socket](QLocalSocket::LocalSocketError code) {
        if (socket_ != socket || code == QLocalSocket::PeerClosedError) return;
        if (code == QLocalSocket::ServerNotFoundError || code == QLocalSocket::ConnectionRefusedError)
            fail("Connect to start the Tailscale service.", State::Stopped);
        else fail("Cannot read Tailscale status: " + socket->errorString().left(256));
    };
#if QT_VERSION >= QT_VERSION_CHECK(5, 15, 0)
    QObject::connect(socket, &QLocalSocket::errorOccurred, this, error);
#else
    QObject::connect(socket, static_cast<void(QLocalSocket::*)(QLocalSocket::LocalSocketError)>(&QLocalSocket::error), this, error);
#endif
    timeout_.start(timeoutMs_);
    socket->connectToServer(path_);
    return true;
}

void Client::readAvailable(QLocalSocket *socket) {
    if (socket_ != socket) return;
    while (socket->bytesAvailable() > 0) {
        wire_.append(socket->read(4096));
        if (wire_.size() > kResponseLimit) { fail("Tailscale response exceeded the size limit."); return; }
    }
}

void Client::fail(const QString &message, State state) {
    Status status; status.state = state; status.error = message;
    finish(status);
}

void Client::finish(const Status &status) {
    if (!socket_) return;
    QLocalSocket *socket = socket_;
    socket_ = nullptr; timeout_.stop();
    socket->abort(); socket->deleteLater();
    if (completed) completed(status);
}

} // namespace tailui
