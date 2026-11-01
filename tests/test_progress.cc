#include <QApplication>
#include <QElapsedTimer>
#include <QFile>
#include <QLabel>
#include <QLocalServer>
#include <QLocalSocket>
#include <QPushButton>
#include <QTemporaryDir>
#include <QThread>
#include <cstdio>
#include <cstdlib>
#include <functional>
#include "controller.h"
#include "panel.h"
#include "qr_widget.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x); std::exit(1); } } while(false)
static bool waitFor(const std::function<bool()> &condition) {
    QElapsedTimer timer;timer.start();
    while(!condition()&&timer.elapsed()<4500){QApplication::processEvents();QThread::msleep(3);}
    return condition();
}
static void write(const QString &path,const QByteArray &data) {
    QFile file(path);CHECK(file.open(QIODevice::WriteOnly));CHECK(file.write(data)==data.size());
}

int main(int argc,char **argv) {
    QApplication app(argc,argv);QTemporaryDir dir;CHECK(dir.isValid());
    const QString socket=dir.path()+"/socket",hold=dir.path()+"/hold",fail=dir.path()+"/fail";
    const QString helper=dir.path()+"/helper",updater=dir.path()+"/updater",routes=dir.path()+"/routes";
    write(routes,"wlan0 00000000 0100000A 0003\n");write(hold,"");
    write(helper,("#!/bin/sh\nwhile [ -f '"+hold+"' ]; do sleep 0.02; done\nif [ -f '"+fail+"' ]; then echo 'Service startup failed'; exit 4; fi\n").toUtf8());
    write(updater,"#!/bin/sh\necho '{}'\n");QFile executable(updater);CHECK(executable.setPermissions(QFile::ReadOwner|QFile::WriteOwner|QFile::ExeOwner));
    tailui::Controller controller(socket,helper,routes,nullptr,updater,dir.path()+"/wireless");
    QWidget window;window.resize(1264,1680);QWidget anchor(&window);anchor.setGeometry(1080,20,80,90);window.show();
    tailui::Panel panel(&window,&controller,&anchor);panel.open();
    auto state=panel.findChild<QLabel*>("tailuiState"),notice=panel.findChild<QLabel*>("tailuiNotice");
    auto primary=panel.findChild<QPushButton*>("tailuiPrimary");auto qr=panel.findChild<tailui::QrWidget*>("tailuiLoginQr");
    CHECK(state&&notice&&primary&&qr);
    CHECK(waitFor([&]{return controller.status().state==tailui::State::Stopped&&controller.hasWifi();}));
    CHECK(controller.act(tailui::Action::SignIn));
    CHECK(waitFor([&]{return notice->text()=="Starting Tailscale service…";}));
    CHECK(primary->text()=="Cancel"&&state->text()=="Connecting");

    QLocalServer server;CHECK(server.listen(socket));
    QByteArray body=R"({"BackendState":"NeedsLogin","Health":["Tailscale is stopped."]})";
    QObject::connect(&server,&QLocalServer::newConnection,&app,[&]{
        QLocalSocket *peer=server.nextPendingConnection();
        QObject::connect(peer,&QLocalSocket::readyRead,peer,[&,peer]{
            if(!peer->readAll().contains("\r\n\r\n"))return;
            peer->write("HTTP/1.0 200 OK\r\nContent-Length: "+QByteArray::number(body.size())+"\r\n\r\n"+body);peer->disconnectFromServer();
        });
    });
    controller.refresh();
    CHECK(waitFor([&]{return notice->text()=="Requesting a sign-in link…";}));
    CHECK(state->text()=="Connecting"&&primary->text()=="Cancel");
    body=R"({"BackendState":"NeedsLogin","Health":["A real startup warning"]})";controller.refresh();
    CHECK(waitFor([&]{return notice->text().contains("Requesting a sign-in link…")&&notice->text().contains("A real startup warning");}));
    body=R"({"BackendState":"Starting"})";controller.refresh();
    CHECK(waitFor([&]{return notice->text()=="Connecting to your tailnet…";}));
    body="malformed";controller.refresh();
    CHECK(waitFor([&]{return controller.status().state==tailui::State::Error;}));
    CHECK(controller.notice().isEmpty()&&!notice->text().isEmpty()&&primary->text()=="Cancel");
    body=R"({"BackendState":"NeedsMachineAuth"})";controller.refresh();
    CHECK(waitFor([&]{return controller.status().state==tailui::State::NeedsLogin&&!controller.status().requiresSignIn;}));
    CHECK(controller.notice().isEmpty());
    body=R"({"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/progress-test"})";controller.refresh();
    CHECK(waitFor([&]{return qr->isVisible();}));CHECK(controller.notice().isEmpty());
    body=R"({"BackendState":"Running","Health":["A real connection warning"]})";controller.refresh();
    CHECK(waitFor([&]{return state->text()=="Connected"&&notice->text()=="A real connection warning";}));
    CHECK(QFile::remove(hold));CHECK(waitFor([&]{return !controller.busy();}));

    server.close();controller.refresh();
    CHECK(waitFor([&]{return controller.status().state==tailui::State::Stopped;}));
    write(hold,"");write(fail,"");CHECK(controller.act(tailui::Action::Connect));
    CHECK(notice->text()=="Starting Tailscale service…");
    CHECK(QFile::remove(hold));
    CHECK(waitFor([&]{return !controller.busy()&&notice->text().contains("Service startup failed");}));
    CHECK(primary->text()!="Cancel");
    std::puts("Startup checkpoints, QR transition and unmasked connection errors passed.");
}
