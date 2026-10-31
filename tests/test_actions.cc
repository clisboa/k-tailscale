#include <QCoreApplication>
#include <QElapsedTimer>
#include <QFile>
#include <QProcess>
#include <QTemporaryDir>
#include <QThread>
#include <cstdio>
#include <cstdlib>
#include <functional>
#include <signal.h>
#include "actions.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x); std::exit(1); } } while(false)
static bool waitFor(const std::function<bool()> &condition) {
    QElapsedTimer timer;timer.start();
    while (!condition() && timer.elapsed()<4000) { QCoreApplication::processEvents();QThread::msleep(5); }
    return condition();
}
static QByteArray read(const QString &path) { QFile f(path);if(!f.open(QIODevice::ReadOnly))return {};return f.readAll(); }

int main(int argc,char **argv) {
    QCoreApplication app(argc,argv);
    QTemporaryDir dir;CHECK(dir.isValid());
    const QString helper=dir.path()+"/control.sh";
    QFile file(helper);CHECK(file.open(QIODevice::WriteOnly));
    file.write(("#!/bin/sh\ncase \"$1\" in\nconnect|signin) echo \"$1\" >> '"+dir.path()+"/calls'; sleep 30 & echo $! > '"+dir.path()+"/child'; wait;;\nstop) echo stop >> '"+dir.path()+"/calls';;\ndisconnect|cancel) echo 'meaningful failure'; exit 7;;\nesac\n").toUtf8());file.close();
    tailui::Actions actions(helper);
    bool done=false,success=false;QString message;
    actions.finished=[&](bool ok,const QString &text){done=true;success=ok;message=text;};
    QProcess sibling;sibling.start("sleep",QStringList{"30"});CHECK(sibling.waitForStarted());
    CHECK(actions.start(tailui::Action::Connect));
    CHECK(!actions.start(tailui::Action::Connect));
    CHECK(waitFor([&]{return QFile::exists(dir.path()+"/child");}));
    CHECK(actions.start(tailui::Action::Stop));
    CHECK(waitFor([&]{return done;}));
    CHECK(success && !actions.busy());
    CHECK(read(dir.path()+"/calls")=="connect\nstop\n");
    CHECK(sibling.state()==QProcess::Running);
    const QString pid=QString::fromLatin1(read(dir.path()+"/child")).trimmed();
    CHECK(waitFor([&]{const QByteArray stat=read("/proc/"+pid+"/stat");return stat.isEmpty()||stat.contains(") Z");}));
    done=false;CHECK(actions.start(tailui::Action::Connect));
    CHECK(actions.start(tailui::Action::Disconnect));
    CHECK(waitFor([&]{return done;}));CHECK(!success && !actions.busy() && message.contains("meaningful failure"));
    done=false;CHECK(actions.start(tailui::Action::Disconnect));
    CHECK(waitFor([&]{return done;}));CHECK(!success && message.contains("meaningful failure"));
    CHECK(QFile::remove(dir.path()+"/child"));
    done=false;CHECK(actions.start(tailui::Action::SignIn));
    CHECK(!actions.start(tailui::Action::Connect)&&!actions.start(tailui::Action::SignIn));
    CHECK(waitFor([&]{return QFile::exists(dir.path()+"/child");}));
    const QString loginPid=QString::fromLatin1(read(dir.path()+"/child")).trimmed();
    CHECK(actions.start(tailui::Action::Cancel));CHECK(waitFor([&]{return done;}));
    CHECK(!success&&message.contains("meaningful failure"));
    CHECK(waitFor([&]{const QByteArray stat=read("/proc/"+loginPid+"/stat");return stat.isEmpty()||stat.contains(") Z");}));
    CHECK(sibling.state()==QProcess::Running);
    const QString stubborn=dir.path()+"/stubborn.sh",lock=dir.path()+"/lock";
    file.setFileName(stubborn);CHECK(file.open(QIODevice::WriteOnly));
    file.write(("#!/bin/sh\ntrap '' TERM\necho $$ > '"+dir.path()+"/stubborn-pid'\nwhile :; do sleep 30; done\n").toUtf8());file.close();
    file.setFileName(helper);CHECK(file.open(QIODevice::WriteOnly|QIODevice::Truncate));
    file.write(("#!/bin/sh\ncase \"$1\" in\nconnect) mkdir '"+lock+"'; trap \"rmdir '"+lock+"'; exit\" TERM; echo $$ > '"+dir.path()+"/group'; /bin/sh '"+stubborn+"' & wait;;\nstop) [ ! -d '"+lock+"' ] || exit 8; echo 'Ready for USB';;\nesac\n").toUtf8());file.close();
    done=false;CHECK(actions.start(tailui::Action::Connect));
    CHECK(waitFor([&]{return QFile::exists(dir.path()+"/stubborn-pid");}));
    const QString stubbornPid=QString::fromLatin1(read(dir.path()+"/stubborn-pid")).trimmed();
    CHECK(actions.start(tailui::Action::Stop));CHECK(waitFor([&]{return done;}));
    const QByteArray survivor=read("/proc/"+stubbornPid+"/stat");
    const bool dead=survivor.isEmpty()||survivor.contains(") Z");
    const int group=read(dir.path()+"/group").trimmed().toInt();if(group>1)::kill(-group,SIGKILL);
    CHECK(dead);CHECK(success&&message.contains("Ready for USB")&&!QFile::exists(lock));
    const QString subgroup=dir.path()+"/subgroup.py";
    file.setFileName(subgroup);CHECK(file.open(QIODevice::WriteOnly));
    file.write(("import os,signal,time\nos.setpgid(0,0)\nsignal.signal(signal.SIGTERM,signal.SIG_IGN)\nopen('"+dir.path()+"/subgroup-pid','w').write(str(os.getpid()))\ntime.sleep(30)\n").toUtf8());file.close();
    file.setFileName(stubborn);CHECK(file.open(QIODevice::WriteOnly|QIODevice::Truncate));
    file.write(("#!/bin/sh\nexec python3 '"+subgroup+"'\n").toUtf8());file.close();
    done=false;CHECK(actions.start(tailui::Action::Connect));
    CHECK(waitFor([&]{return QFile::exists(dir.path()+"/subgroup-pid");}));
    const QString subgroupPid=QString::fromLatin1(read(dir.path()+"/subgroup-pid")).trimmed();
    CHECK(actions.start(tailui::Action::Stop));CHECK(waitFor([&]{return done;}));
    const QByteArray subgroupStat=read("/proc/"+subgroupPid+"/stat");
    const bool subgroupDead=subgroupStat.isEmpty()||subgroupStat.contains(") Z");
    const int subgroupId=subgroupPid.toInt();if(subgroupId>1)::kill(-subgroupId,SIGKILL);
    CHECK(subgroupDead);CHECK(success&&message.contains("Ready for USB")&&!QFile::exists(lock));
    sibling.terminate();CHECK(sibling.waitForFinished());
    std::puts("Owned process, duplicate action, cancellation and error tests passed.");
}
