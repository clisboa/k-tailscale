#include <QCoreApplication>
#include <QElapsedTimer>
#include <QFile>
#include <QTemporaryDir>
#include <QThread>
#include <cstdio>
#include <cstdlib>
#include <functional>
#include "updates.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x); std::exit(1); } } while(false)
static bool waitFor(const std::function<bool()> &condition) {
    QElapsedTimer timer;timer.start();
    while(!condition()&&timer.elapsed()<4500){QCoreApplication::processEvents();QThread::msleep(3);}
    return condition();
}
static void write(const QString &path,const QByteArray &data) {
    QFile file(path);CHECK(file.open(QIODevice::WriteOnly));CHECK(file.write(data)==data.size());
}
static QByteArray read(const QString &path) {
    QFile file(path);CHECK(file.open(QIODevice::ReadOnly));return file.readAll();
}
static void overlap(const QString &verb,bool failProgress) {
    QTemporaryDir dir;CHECK(dir.isValid());
    const QString helper=dir.path()+"/helper",status=dir.path()+"/status",hold=dir.path()+"/hold",started=dir.path()+"/started",calls=dir.path()+"/calls";
    write(status,R"({"current":"1.104.4","addon_available":true,"restart":false})");
    const QString script="#!/bin/sh\necho \"$1\" >> '"+calls+"'\npayload=$(cat '"+status+"')\nif [ \"$1\" = '"+verb+"' ] && [ -f '"+hold+"' ]; then\n touch '"+started+"'\n while [ -f '"+hold+"' ]; do sleep 0.02; done\n "+(failProgress?QString("exit 7"):QString(":"))+"\nfi\ncase \"$1\" in status) printf '%s\\n' \"$payload\";; progress) echo '{\"progress\":\"Installing\"}';; esac\n";
    write(helper,script.toUtf8());QFile executable(helper);CHECK(executable.setPermissions(QFile::ReadOwner|QFile::WriteOwner|QFile::ExeOwner));
    tailui::Updates updates(helper);CHECK(updates.load());
    CHECK(waitFor([&]{return !updates.busy()&&updates.status().value("addon_available").toBool();}));
    write(hold,"");CHECK(verb=="progress"?updates.progress():updates.load());
    CHECK(waitFor([&]{return QFile::exists(started);}));
    // The install finishes while a progress/stale-status response is held.
    write(status,R"({"current":"1.104.4","addon_available":false,"restart":true})");
    updates.load();updates.load();updates.load();
    CHECK(QFile::remove(hold));
    CHECK(waitFor([&]{return !updates.busy()&&updates.status().value("restart").toBool();}));
    CHECK(!updates.status().value("addon_available").toBool());
    const int expectedStatusCalls=verb=="status"?3:2;
    CHECK(read(calls).split('\n').count("status")==expectedStatusCalls);
}
int main(int argc,char **argv) {
    QCoreApplication app(argc,argv);
    overlap("progress",false);overlap("progress",true);overlap("status",false);
    std::puts("Coalesced status refresh after progress, failure and stale status passed.");
}
