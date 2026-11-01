#pragma once
#include <QObject>
#include <QProcess>
#include <QTimer>
#include <QElapsedTimer>
#include <QJsonObject>
#include <functional>

namespace tailui {
class Updates : public QObject {
public:
    explicit Updates(const QString &helper, QObject *parent=nullptr);
    ~Updates() override;
    void watch(bool eligible);
    bool check(bool automatic=false);
    bool load();
    bool progress();
    bool setAutomatic(bool enabled);
    bool busy() const { return process_.state()!=QProcess::NotRunning; }
    QJsonObject status() const { return status_; }
    QString message() const { return message_; }
    std::function<void()> changed;
private:
    bool launch(const QString &verb);
    void drain();
    QString helper_, verb_, message_;
    QProcess process_;
    QTimer timer_, deadline_;
    QElapsedTimer lastAutoCheck_;
    QByteArray output_;
    QJsonObject status_;
    bool watching_=false, oversized_=false, pendingStatus_=false;
};
}
