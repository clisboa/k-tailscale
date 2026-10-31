#include <QCoreApplication>
#include <QElapsedTimer>
#include <QLocalServer>
#include <QLocalSocket>
#include <QTemporaryDir>
#include <QThread>
#include <QTimer>
#include <QVariant>
#include <cstdio>
#include <cstdlib>
#include <functional>
#include "client.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr,"FAIL line %d: %s\n",__LINE__,#x); std::exit(1); } } while(false)
static bool waitFor(const std::function<bool()> &condition) {
    QElapsedTimer timer;timer.start();
    while (!condition() && timer.elapsed()<4000) { QCoreApplication::processEvents();QThread::msleep(2); }
    return condition();
}
int main(int argc,char **argv) {
    QCoreApplication app(argc,argv);QTemporaryDir dir;CHECK(dir.isValid());
    QLocalServer server;CHECK(server.listen(dir.path()+"/socket"));
    int mode=0,requests=0;QByteArray lastRequest;
    QObject::connect(&server,&QLocalServer::newConnection,&app,[&]{
        QLocalSocket *peer=server.nextPendingConnection();
        QObject::connect(peer,&QLocalSocket::readyRead,peer,[&,peer]{
            if(peer->property("handled").toBool()) {peer->readAll();return;}
            QByteArray request=peer->property("request").toByteArray()+peer->readAll();
            peer->setProperty("request",request);
            if(!request.contains("\r\n\r\n"))return;
            peer->setProperty("handled",true);lastRequest=request;++requests;
            if(mode==1)return;
            QByteArray response="HTTP/1.0 200 OK\r\nContent-Length: 26\r\n\r\n{\"BackendState\":\"Running\"}";
            if(mode==2)response="HTTP/1.0 200 OK\r\nContent-Length: 20\r\n\r\n{}";
            if(mode==3)response="HTTP/1.0 200 OK\r\n\r\nnot JSON";
            if(mode==4)response="HTTP/1.0 200 OK\r\n\r\n"+QByteArray(1024*1024+1,'x');
            peer->write(response.left(12));
            QTimer::singleShot(15,peer,[peer,response]{peer->write(response.mid(12));peer->disconnectFromServer();});
        });
    });
    tailui::Client client(dir.path()+"/socket",nullptr,100);
    tailui::Status result;bool done=false;int completions=0;
    client.completed=[&](const tailui::Status &status){result=status;done=true;++completions;};
    CHECK(client.refresh());CHECK(!client.refresh());CHECK(waitFor([&]{return done;}));
    CHECK(result.state==tailui::State::Connected && requests==1 && completions==1);
    CHECK(lastRequest.startsWith("GET /localapi/v0/status?peers=false HTTP/1.0\r\n"));
    CHECK(lastRequest.contains("Host: local-tailscaled.sock\r\n")&&lastRequest.contains("Connection: close\r\n"));
    for(mode=1;mode<=4;++mode){
        done=false;CHECK(client.refresh());CHECK(waitFor([&]{return done;}));
        CHECK(result.state==tailui::State::Error && !result.error.isEmpty());
    }
    CHECK(completions==5);
    CHECK(lastRequest.startsWith("GET /localapi/v0/status?peers=false HTTP/1.0\r\n"));
    tailui::Client missing(dir.path()+"/missing",nullptr,100);done=false;
    missing.completed=[&](const tailui::Status &status){result=status;done=true;};
    CHECK(missing.refresh());CHECK(waitFor([&]{return done;}));CHECK(result.state==tailui::State::Stopped);
    std::puts("Real Unix-socket fragmentation, timeout, bounds and single-request tests passed.");
}
