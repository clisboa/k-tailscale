#pragma once

#include "actions.h"
#include "updates.h"
#include "client.h"
#include <QPointer>
#include <QSet>
#include <QTimer>
#include <functional>

namespace tailui {

class Controller : public QObject {
public:
    explicit Controller(const QString &socket = "/var/run/tailscale/tailscaled.sock",
                        const QString &helper = "/usr/local/tailscale-ui/control.sh",
                        const QString &routes = "/proc/net/route", QObject *parent = nullptr,
                        const QString &updateHelper = "/usr/local/k-tailscale/helper");
    void observe(QObject *owner, const std::function<void()> &callback);
    void setBarVisible(QObject *bar, bool visible);
    void setPanelVisible(QObject *panel, bool visible);
    void refresh();
    bool act(Action action);
    bool canSignOut() const;
    Status status() const;
    QString notice() const { return notice_; }
    Updates &updates() { return updates_; }
    bool busy() const { return actions_.busy(); }
    bool connecting() const { return actions_.busy() && isConnectionAction(actions_.current()); }
    bool hasWifi() const { return wifi_; }
    bool refreshing() const { return client_.busy(); }
    bool runtimeReady() const { return runtimeReady_; }
    bool preparing() const { return preparing_; }
    bool prepareRuntime();

private:
    struct Observer { QPointer<QObject> owner; std::function<void()> callback; };
    void watch(QObject *owner);
    void schedule();
    void notify();
    Client client_;
    Actions actions_;
    Updates updates_;
    QTimer poll_;
    QString routePath_, notice_, retiredAuthUrl_;
    Status status_;
    bool wifi_ = false, pending_ = false, usbReady_ = false;
    bool runtimeReady_ = true, preparing_ = false, signedOut_ = false, awaitingLogin_ = false, connectionFailureNotice_ = false;
    QSet<QObject *> tracked_, bars_, panels_;
    QList<Observer> observers_;
};

} // namespace tailui
