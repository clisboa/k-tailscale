#include "controller.h"
#include "log.h"
#include <QFile>
#include <QJsonDocument>
#include <QJsonObject>

namespace tailui {

Controller::Controller(const QString &socket, const QString &helper, const QString &routes, QObject *parent, const QString &updateHelper, const QString &wireless)
    : QObject(parent), client_(socket), actions_(helper), updates_(updateHelper), routePath_(routes), wirelessPath_(wireless) {
    updates_.changed=[this]{notify();};
    status_.state = State::Stopped;
    client_.completed = [this](const Status &status) {
        if (usbReady_ && status.state != State::Stopped) {
            usbReady_ = false; notice_.clear();
        }
        status_ = status;
        // LocalAPI can retain AuthURL after logout. Remember that retired URL
        // even when it arrives in a new request, not only an old in-flight one.
        if(signedOut_&&!status.authUrl.isEmpty())retiredAuthUrl_=status.authUrl;
        if(awaitingLogin_&&!signedOut_&&status.state==State::Connected&&status.hasLogin){
            awaitingLogin_=false;retiredAuthUrl_.clear();
        }
        const Status displayed=this->status();
        if(connectionFailureNotice_&&(status.state==State::Connected||!displayed.authUrl.isEmpty()||
            (displayed.state==State::NeedsLogin&&!displayed.requiresSignIn))){
            // The URL or connection can arrive after the command's last poll.
            // Retire only connection failures, leaving setup/update errors intact.
            notice_.clear();connectionFailureNotice_=false;
        }
        notify();
        if (pending_) { pending_ = false; refresh(); }
    };
    actions_.changed = [this] { notify(); };
    actions_.finished = [this](bool ok, const QString &raw) {
        const QJsonDocument result=QJsonDocument::fromJson(raw.toUtf8());
        const QString message=result.isObject()?result.object().value(ok?"message":"error").toString(raw):raw;
        if(actions_.current()==Action::SignOut){
            // No request from before logout may restore the previous profile.
            client_.cancel();pending_=false;
            if(ok){status_=Status();status_.state=State::NeedsLogin;signedOut_=true;awaitingLogin_=true;}
            notice_=ok?"Signed out. Tap Sign in to continue.":message;
            notify();refresh();return;
        }
        if(actions_.current()==Action::InstallTailscale||actions_.current()==Action::InstallAddon){
            notice_=ok?(actions_.current()==Action::InstallAddon?"Addon installed. Open Updates to restart the Kobo.":"Tailscale update installed."):message;
            updates_.load();notify();refresh();return;
        }
        if(actions_.current()==Action::Prepare) {
            preparing_=false;runtimeReady_=ok;
            notice_=ok&&!message.contains("startup failed")?QString():message;
            if(!ok)notice_+="\nStorage setup did not finish. Retry setup after addressing the error.";
            log(ok?"Runtime preparation completed.":"Runtime preparation failed: "+message);
            schedule();notify();if(ok)refresh();return;
        }
        connectionFailureNotice_=!ok&&isConnectionAction(actions_.current());
        notice_ = ok && isConnectionAction(actions_.current()) ? QString() : message;
        const Status displayed=this->status();
        if (!ok && isConnectionAction(actions_.current()) && displayed.state == State::NeedsLogin &&
            (!displayed.authUrl.isEmpty() || !displayed.requiresSignIn)) {
            // The panel derives authentication instructions from current status.
            // Persisting them here would outlive device approval or sign-in.
            notice_.clear();
            connectionFailureNotice_=false;
        }
        if (ok && actions_.current() == Action::Stop) {
            // A response begun before Stop is stale. Dispose it before starting
            // a fresh check; any later daemon evidence invalidates readiness.
            client_.cancel(); pending_ = false; usbReady_ = true;
            status_ = Status(); status_.state = State::Stopped;
        }
        notify(); refresh();
    };
    QObject::connect(&poll_, &QTimer::timeout, this, [this] { refresh(); });
}

void Controller::observe(QObject *owner, const std::function<void()> &callback) {
    Observer observer; observer.owner = owner; observer.callback = callback;
    observers_.append(observer);
}

void Controller::watch(QObject *owner) {
    if (tracked_.contains(owner)) return;
    tracked_.insert(owner);
    QObject::connect(owner, &QObject::destroyed, this, [this, owner] {
        tracked_.remove(owner); bars_.remove(owner); panels_.remove(owner); schedule();
    });
}

void Controller::setBarVisible(QObject *bar, bool visible) {
    watch(bar);
    if (visible) bars_.insert(bar); else bars_.remove(bar);
    schedule();
    if (visible) refresh();
}

void Controller::setPanelVisible(QObject *panel, bool visible) {
    watch(panel);
    if (visible) panels_.insert(panel); else panels_.remove(panel);
    schedule();
    if (visible) refresh();
}

void Controller::schedule() {
    if(!runtimeReady_||preparing_||actions_.busy()||(bars_.isEmpty()&&panels_.isEmpty()))updates_.watch(false);
    const int interval = !runtimeReady_||preparing_ ? 0 : !panels_.isEmpty() ? 3000 : !bars_.isEmpty() ? 15000 : 0;
    if (!interval) poll_.stop();
    else if (!poll_.isActive() || poll_.interval() != interval) poll_.start(interval);
}

void Controller::refresh() {
    if(!runtimeReady_||preparing_)return;
    if(actions_.busy()&&actions_.current()==Action::SignOut)return;
    QFile routes(routePath_), wireless(wirelessPath_);
    const QByteArray wirelessData=wireless.open(QIODevice::ReadOnly)?wireless.read(65536):QByteArray();
    wifi_ = routes.open(QIODevice::ReadOnly) && wifiAvailable(routes.read(65536),wirelessData);
    updates_.watch((!bars_.isEmpty()||!panels_.isEmpty())&&wifi_&&!actions_.busy());
    if(actions_.busy()&&(actions_.current()==Action::InstallTailscale||actions_.current()==Action::InstallAddon))updates_.progress();
    if (!client_.refresh()) pending_ = true;
}

Status Controller::status() const {
    Status view = status_;
    if(signedOut_){view=Status();view.state=State::NeedsLogin;view.requiresSignIn=true;}
    if(awaitingLogin_){
        view.name.clear();view.address.clear();view.hasLogin=false;
        // Unknown service/startup status cannot erase a confirmed logout.
        // NeedsMachineAuth shares the display state but remains approval only.
        if(view.state!=State::NeedsLogin)view.requiresSignIn=true;
    }
    if(!retiredAuthUrl_.isEmpty()&&view.authUrl==retiredAuthUrl_){view.authUrl.clear();view.health.clear();}
    // AuthURL can outlive login in LocalAPI. Clear it before the Wi-Fi view
    // override so a completed sign-in cannot reappear as an offline QR/link.
    if(view.state==State::Connected||view.state==State::Disconnected||view.state==State::Stopped)view.authUrl.clear();
    if (connecting() && view.state != State::Connected && view.state != State::Error) {
        // These idle warnings become stale while a connection is in flight.
        // Preserve all other health messages, including firewall failures.
        view.health.removeAll("Tailscale is stopped.");
        view.health.removeAll("You are logged out.");
        if(view.state!=State::NeedsLogin||(view.requiresSignIn&&view.authUrl.isEmpty()))view.state=State::Connecting;
    }
    if (!wifi_ && view.state != State::Stopped && view.state != State::Error) view.state = State::NoWifi;
    return view;
}

QString Controller::notice() const {
    if(!notice_.isEmpty()||!connecting()||!wifi_)return notice_;
    const Status displayed=status();
    if(status_.state==State::Connected||status_.state==State::Error||!displayed.authUrl.isEmpty()||
        (displayed.state==State::NeedsLogin&&!displayed.requiresSignIn))return QString();
    QString progress=status_.state==State::Stopped?"Starting Tailscale service…":
        status_.state==State::NeedsLogin?"Requesting a sign-in link…":"Connecting to your tailnet…";
    if(!displayed.health.isEmpty())progress+="\n"+displayed.health.join("\n");
    return progress;
}

bool Controller::canSignOut() const {
    return runtimeReady_&&!preparing_&&!signedOut_&&!awaitingLogin_&&!actions_.busy()&&!updates_.busy()&&
        status_.hasLogin&&status_.state!=State::Error&&status_.state!=State::Stopped;
}

bool Controller::act(Action action) {
    if(!runtimeReady_||preparing_||action==Action::Prepare)return false;
    if(action==Action::SignOut&&!canSignOut())return false;
    if (isConnectionAction(action) && !wifi_) {
        connectionFailureNotice_=false;
        notice_ = "Connect Wi-Fi using the Kobo's Wi-Fi control, then try again.";
        notify(); return false;
    }
    if((action==Action::InstallTailscale||action==Action::InstallAddon)&&(actions_.busy()||updates_.busy()))return false;
    updates_.watch(false);
    if(action==Action::SignOut){retiredAuthUrl_=status_.authUrl;client_.cancel();pending_=false;}
    if(isConnectionAction(action)&&signedOut_){
        client_.cancel();pending_=false;status_=Status();status_.state=State::Connecting;signedOut_=false;
    }
    notice_.clear();connectionFailureNotice_=false; usbReady_ = false;
    if (!actions_.start(action)) { notice_ = "Another action is still in progress."; notify(); return false; }
    return true;
}

bool Controller::prepareRuntime() {
    if(actions_.busy())return false;
    preparing_=true;runtimeReady_=false;usbReady_=false;connectionFailureNotice_=false;
    client_.cancel();pending_=false;status_=Status();
    notice_="Preparing Tailscale…";schedule();notify();
    if(actions_.start(Action::Prepare))return true;
    preparing_=false;notice_="Another Tailscale action is still stopping. Retry setup shortly.";notify();return false;
}

void Controller::notify() {
    for (int i = observers_.size() - 1; i >= 0; --i) if (!observers_[i].owner) observers_.removeAt(i);
    const QList<Observer> observers = observers_;
    for (const Observer &observer : observers) if (observer.owner && observer.callback) observer.callback();
}

} // namespace tailui
