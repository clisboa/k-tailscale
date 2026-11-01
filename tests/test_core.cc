#include <QCoreApplication>
#include <QJsonArray>
#include <QJsonDocument>
#include <QJsonObject>
#include <cstdio>
#include <cstdlib>
#include "model.h"

#define CHECK(x) do { if (!(x)) { std::fprintf(stderr, "FAIL line %d: %s\n", __LINE__, #x); std::exit(1); } } while (false)

using namespace tailui;

int main(int argc, char **argv) {
    QCoreApplication app(argc, argv);
    const Status connected = parseStatus(R"({"BackendState":"Running","Self":{"HostName":"reader","TailscaleIPs":["fd7a::1","100.64.0.9"]},"Health":["A health warning"],"Peer":{"node":{"HostName":"<b>nas</b>","TailscaleIPs":["100.64.0.2"],"Online":true}}})");
    CHECK(connected.state == State::Connected);
    CHECK(connected.name == "reader" && connected.address == "100.64.0.9");
    CHECK(connected.health == QStringList{"A health warning"});
    CHECK(parseStatus(R"({"BackendState":"Running","Peer":{"bad":3}})").state==State::Connected);
    CHECK(parseStatus(R"({"BackendState":"Stopped"})").state == State::Disconnected);
    CHECK(parseStatus(R"({"BackendState":"Starting"})").state == State::Connecting);
    CHECK(parseStatus(R"({"BackendState":"NeedsLogin","AuthURL":"https://login.tailscale.com/a/example"})").authUrl == "https://login.tailscale.com/a/example");
    CHECK(parseStatus(R"({"BackendState":"NeedsLogin","AuthURL":"javascript:bad"})").authUrl.isEmpty());
    const QString fullUrl="https://login.tailscale.com/a/"+QString(2500,'a');
    QJsonObject login;login.insert("BackendState","NeedsLogin");login.insert("AuthURL",fullUrl);
    CHECK(parseStatus(QJsonDocument(login).toJson()).authUrl==fullUrl);
    CHECK(parseStatus(R"({"BackendState":"NeedsMachineAuth"})").state == State::NeedsLogin);
    CHECK(parseStatus("bad").state == State::Error);
    CHECK(parseStatus(QByteArray(kResponseLimit + 1, ' ')).state == State::Error);
    CHECK(parseStatus("[]").state == State::Error);
    CHECK(parseStatus(R"({"BackendState":12})").state == State::Error);
    CHECK(parseStatus(R"({"BackendState":"FutureState"})").state == State::Error);
    CHECK(parseStatus(R"({"BackendState":"Running","Self":{"TailscaleIPs":["999.1.2.3","100.64.0.7"]}})").address == "100.64.0.7");

    QByteArray body; QString error;
    CHECK(decodeResponse("HTTP/1.0 200 OK\r\nContent-Length: 2\r\n\r\n{}", &body, &error) && body == "{}");
    CHECK(decodeResponse("HTTP/1.0 200 OK\r\nConnection: close\r\n\r\n{}", &body, &error));
    CHECK(!decodeResponse("HTTP/1.0 403 Forbidden\r\n\r\nsecret", &body, &error));
    CHECK(error.contains("403") && !error.contains("secret"));
    CHECK(!decodeResponse("HTTP/1.0 200 OK\r\nContent-Length: 3\r\n\r\n{}", &body, &error));
    CHECK(!decodeResponse("HTTP/1.0 200 OK\r\nContent-Length: 1\r\n\r\n{}", &body, &error));
    CHECK(!decodeResponse("HTTP/1.0 200 OK\r\nContent-Length: -1\r\n\r\n{}", &body, &error));
    CHECK(!decodeResponse("HTTP/1.0 200 OK\r\nContent-Length: 2\r\nContent-Length: 2\r\n\r\n{}", &body, &error));
    CHECK(!decodeResponse("HTTP/1.0 200 OK\r\nTransfer-Encoding: chunked\r\n\r\n0\r\n\r\n", &body, &error));
    CHECK(!decodeResponse("HTTP/1.0 200 OK\r\nBroken\r\n\r\n{}", &body, &error));
    CHECK(!decodeResponse(QByteArray(1024 * 1024 + 1, 'x'), &body, &error));
    CHECK(!decodeResponse("HTTP/1.0 200 OK\r\nX: " + QByteArray(17000, 'x') + "\r\n\r\n{}", &body, &error));
    CHECK(!decodeResponse("not http\r\n\r\n{}", &body, &error));
    CHECK(wifiAvailable("Iface Destination Gateway Flags\nwlan0 00000000 0100A8C0 0003\n"));
    CHECK(!wifiAvailable("wlan0 00000000 0100A8C0 0000\n"));
    CHECK(!wifiAvailable("eth0 00000000 0100A8C0 0003\n"));
    const QByteArray legacyWireless="Inter-| sta-| Quality\n eth0: 0000 70. -40. -256 0 0 0 0 0 0\n";
    CHECK(wifiAvailable("eth0 00000000 0100A8C0 0003\n",legacyWireless));
    CHECK(!wifiAvailable("eth0 00000000 0100A8C0 0001\n",legacyWireless));
    CHECK(!wifiAvailable("tailscale0 00000000 0100A8C0 0003\n",legacyWireless));
    CHECK(!wifiAvailable("usb0 00000000 0100A8C0 0003\n",legacyWireless));
    CHECK(!wifiAvailable("wlan0 00000000 00000000 0001\n"));
    std::puts("Core status and HTTP boundary tests passed.");
    return 0;
}
