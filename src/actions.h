#pragma once

#include <QObject>
#include <QProcess>
#include <QTimer>
#include <QElapsedTimer>
#include <functional>

namespace tailui {

enum class Action { Connect, SignIn, Disconnect, SignOut, Cancel, Stop, Prepare, InstallTailscale, InstallAddon, Restart };
inline bool isConnectionAction(Action action) { return action==Action::Connect||action==Action::SignIn; }

class OwnedProcess : public QProcess {
protected:
    void setupChildProcess() override;
};

class Actions : public QObject {
public:
    explicit Actions(const QString &helper = "/usr/local/tailscale-ui/control.sh", QObject *parent = nullptr);
    ~Actions() override;
    bool start(Action action);
    bool busy() const { return busy_; }
    Action current() const { return current_; }
    std::function<void()> changed;
    std::function<void(bool, const QString &)> finished;

private:
    void launch(Action action);
    void drain();
    void complete(int code, QProcess::ExitStatus status);
    void terminateOwned(int signal);
    bool sessionRunning(bool *verified, QList<qint64> *groups = nullptr) const;
    void settleCancellation();
    QString helper_;
    OwnedProcess process_;
    QTimer killTimer_;
    QElapsedTimer cancelClock_;
    qint64 session_ = 0;
    QByteArray output_;
    bool busy_ = false, queued_ = false;
    bool leaderExited_ = false, killed_ = false, blocked_ = false;
    Action current_ = Action::Disconnect, next_ = Action::Disconnect;
};

} // namespace tailui
