#include "actions.h"

#include <signal.h>
#include <unistd.h>
#include <cerrno>
#include <QDir>
#include <QFile>

namespace tailui {
namespace {
QString command(Action action) {
    switch (action) {
    case Action::Connect: return "connect";
    case Action::SignIn: return "signin";
    case Action::Disconnect: return "disconnect";
    case Action::SignOut: return "logout";
    case Action::Cancel: return "cancel";
    case Action::Stop: return "stop";
    case Action::Prepare: return "prepare";
    case Action::InstallTailscale: return "install";
    case Action::InstallAddon: return "install-addon";
    case Action::Restart: return "restart";
    }
    return QString();
}
}

void OwnedProcess::setupChildProcess() {
    if (::setsid() < 0) ::_exit(126);
}

Actions::Actions(const QString &helper, QObject *parent) : QObject(parent), helper_(helper) {
    process_.setWorkingDirectory("/");
    process_.setProcessChannelMode(QProcess::MergedChannels);
    QObject::connect(&process_, &QProcess::readyReadStandardOutput, this, [this] { drain(); });
    QObject::connect(&process_, &QProcess::started, this, [this] {
#if QT_VERSION >= QT_VERSION_CHECK(5, 3, 0)
        const pid_t pid = static_cast<pid_t>(process_.processId());
#else
        const pid_t pid = static_cast<pid_t>(process_.pid());
#endif
        // setupChildProcess establishes this session before exec. Retain its ID
        // after the shell exits: descendants can own multiple process groups.
        if (pid > 1 && pid != ::getpid() && pid != ::getpgrp()) {
            const pid_t actual = ::getpgid(pid);
            if (actual == pid || (actual < 0 && errno == ESRCH)) session_ = pid;
        }
        if (queued_) terminateOwned(SIGTERM);
    });
    QObject::connect(&process_, static_cast<void(QProcess::*)(int,QProcess::ExitStatus)>(&QProcess::finished),
                     this, [this](int code, QProcess::ExitStatus status) { complete(code, status); });
    auto failed = [this](QProcess::ProcessError error) {
        if (error == QProcess::FailedToStart && busy_) {
            output_ = process_.errorString().toUtf8();
            complete(-1, QProcess::CrashExit);
        }
    };
#if QT_VERSION >= QT_VERSION_CHECK(5, 6, 0)
    QObject::connect(&process_, &QProcess::errorOccurred, this, failed);
#else
    QObject::connect(&process_, static_cast<void(QProcess::*)(QProcess::ProcessError)>(&QProcess::error), this, failed);
#endif
    QObject::connect(&killTimer_, &QTimer::timeout, this, [this] { settleCancellation(); });
}

Actions::~Actions() {
    changed = nullptr; finished = nullptr; queued_ = false;
    killTimer_.stop();
    process_.disconnect(this);
    terminateOwned(SIGTERM);
    if (process_.state() != QProcess::NotRunning) process_.waitForFinished(250);
    terminateOwned(SIGKILL);
    if (process_.state() != QProcess::NotRunning) process_.waitForFinished(250);
}

bool Actions::start(Action action) {
    if (command(action).isEmpty()) return false;
    if (blocked_) {
        if (process_.state() != QProcess::NotRunning) return false;
        bool verified = false;
        if (sessionRunning(&verified) || !verified) return false;
        blocked_ = false; session_ = 0;
    }
    if (busy_) {
        if (!isConnectionAction(current_) || isConnectionAction(action)) return false;
        if (queued_ && next_ == Action::Stop) return action == Action::Stop;
        next_ = action;
        if (!queued_) {
            queued_ = true; killed_ = false; cancelClock_.start();
            terminateOwned(SIGTERM); killTimer_.start(50);
        }
        if (changed) changed();
        return true;
    }
    launch(action);
    return true;
}

void Actions::launch(Action action) {
    current_ = action; busy_ = true; output_.clear(); session_ = 0; leaderExited_ = false;
    if (changed) changed();
    process_.start("/bin/sh", QStringList{helper_, command(action)});
}

void Actions::terminateOwned(int signal) {
    bool verified = false;
    if (session_ > 1 && session_ != ::getpid() && session_ != ::getpgrp()) {
        QList<qint64> groups;
        if (sessionRunning(&verified, &groups) && verified) {
            for (qint64 group : groups) {
                if (group > 1 && group != ::getpid() && group != ::getpgrp())
                    ::kill(-static_cast<pid_t>(group), signal);
            }
        }
        return;
    }
#if QT_VERSION >= QT_VERSION_CHECK(5, 3, 0)
    const pid_t pid = static_cast<pid_t>(process_.processId());
#else
    const pid_t pid = static_cast<pid_t>(process_.pid());
#endif
    if (pid <= 1 || pid == ::getpid()) return;
    if (::getpgid(pid) == pid) ::kill(-pid, signal);
    else if (signal == SIGKILL) process_.kill();
    else process_.terminate();
}

bool Actions::sessionRunning(bool *verified, QList<qint64> *groups) const {
    *verified = true;
    if (groups) groups->clear();
    if (!session_) return false;
    const QDir proc("/proc");
    if (!proc.exists() || !proc.isReadable()) { *verified = false; return true; }
    bool sawMember = false, running = false;
    for (const QString &entry : proc.entryList(QDir::Dirs | QDir::NoDotAndDotDot)) {
        bool numeric = false; entry.toInt(&numeric); if (!numeric) continue;
        QFile stat(proc.filePath(entry + "/stat"));
        if (!stat.open(QIODevice::ReadOnly)) continue;
        const QByteArray data = stat.read(4096);
        const int end = data.lastIndexOf(") "); if (end < 0) continue;
        const QList<QByteArray> fields = data.mid(end + 2).simplified().split(' ');
        if (fields.size() < 4) continue;
        const qint64 group = fields[2].toLongLong();
        if (fields[3].toLongLong() != session_) {
            if (group == session_) { *verified = false; return true; }
            continue;
        }
        sawMember = true;
        if (fields[0] != "Z" && fields[0] != "X") {
            if (group <= 1 || group == ::getpid() || group == ::getpgrp()) { *verified = false; return true; }
            running = true;
            if (groups && !groups->contains(group)) groups->append(group);
        }
    }
    // Zombies hold no file descriptors. If a group exists but /proc revealed
    // no members, its termination could not be verified; fail closed.
    if (!sawMember && (::kill(-static_cast<pid_t>(session_), 0) == 0 || errno != ESRCH)) {
        *verified = false; return true;
    }
    return running;
}

void Actions::settleCancellation() {
    if (!queued_) return;
    bool verified = false;
    const bool running = sessionRunning(&verified);
    if (leaderExited_ && verified && !running) {
        killTimer_.stop(); busy_ = false; session_ = 0;
        const Action action = next_; queued_ = false; launch(action); return;
    }
    if (!killed_ && cancelClock_.elapsed() >= 1500) {
        terminateOwned(SIGKILL); killed_ = true;
    }
    if (cancelClock_.elapsed() >= 3500) {
        killTimer_.stop(); queued_ = false; busy_ = false; blocked_ = true;
        if (changed) changed();
        if (finished) finished(false, "Could not verify that connection processes stopped. Restart the Kobo before USB.");
    }
}

void Actions::drain() {
    while (process_.bytesAvailable() > 0) {
        output_.append(process_.read(4096));
        if (output_.size() > 8192) output_ = output_.right(8192);
    }
}

void Actions::complete(int code, QProcess::ExitStatus status) {
    if (!busy_) return;
    drain(); leaderExited_ = true;
    if (queued_) {
        settleCancellation(); return;
    }
    killTimer_.stop(); busy_ = false;
    if (blocked_) return;
    session_ = 0;
    const bool ok = status == QProcess::NormalExit && code == 0;
    QString message = QString::fromUtf8(output_).trimmed().left(2048);
    if (message.isEmpty()) message = ok ? "Action completed." : QString("Tailscale command failed (status %1).").arg(code);
    if (changed) changed();
    if (finished) finished(ok, message);
}

} // namespace tailui
