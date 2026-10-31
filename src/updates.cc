#include "updates.h"
#include <QJsonDocument>
#include <QJsonParseError>

namespace tailui {
Updates::Updates(const QString &helper,QObject *parent):QObject(parent),helper_(helper) {
    process_.setWorkingDirectory("/");process_.setProcessChannelMode(QProcess::MergedChannels);
    QObject::connect(&process_,&QProcess::readyReadStandardOutput,this,[this]{drain();});
    QObject::connect(&process_,static_cast<void(QProcess::*)(int,QProcess::ExitStatus)>(&QProcess::finished),this,[this](int code,QProcess::ExitStatus exit){
        drain();deadline_.stop();const bool check=verb_!="status"&&verb_!="progress";
        const QJsonDocument doc=QJsonDocument::fromJson(output_);
        if(oversized_)message_="Update response was too large.";
        else if(exit!=QProcess::NormalExit||code!=0)message_=doc.isObject()?doc.object().value("error").toString():"Could not check updates. Try again on Wi-Fi.";
        else if(verb_=="progress"){if(doc.isObject())status_.insert("progress",doc.object().value("progress"));}
        else if(!check){if(doc.isObject()&&doc.object().contains("current")){status_=doc.object();message_.clear();}else message_="Invalid update status response.";}
        if(changed)changed();
        if(check)load();
    });
    auto failed=[this](QProcess::ProcessError e){if(e==QProcess::FailedToStart){deadline_.stop();message_="Update helper is unavailable. Retry setup.";if(changed)changed();}};
#if QT_VERSION >= QT_VERSION_CHECK(5,6,0)
    QObject::connect(&process_,&QProcess::errorOccurred,this,failed);
#else
    QObject::connect(&process_,static_cast<void(QProcess::*)(QProcess::ProcessError)>(&QProcess::error),this,failed);
#endif
    deadline_.setSingleShot(true);
    QObject::connect(&deadline_,&QTimer::timeout,this,[this]{process_.kill();});
    timer_.setInterval(15*60*1000);
    QObject::connect(&timer_,&QTimer::timeout,this,[this]{timer_.setInterval(15*60*1000);if(watching_)check(true);});
}
Updates::~Updates(){changed=nullptr;process_.disconnect(this);if(busy()){process_.terminate();if(!process_.waitForFinished(500)){process_.kill();process_.waitForFinished(500);}}}
void Updates::watch(bool eligible){
    if(watching_==eligible)return;
    watching_=eligible;
    if(!eligible){timer_.stop();return;}
    const int interval=15*60*1000;
    const qint64 elapsed=lastAutoCheck_.isValid()?lastAutoCheck_.elapsed():interval;
    if(elapsed>=interval){timer_.start(interval);check(true);}
    else timer_.start(static_cast<int>(qMax<qint64>(1000,interval-elapsed)));
}
bool Updates::check(bool automatic){const bool started=launch(automatic?"check-auto":"check");if(started&&automatic)lastAutoCheck_.start();return started;}
bool Updates::progress(){return launch("progress");}
bool Updates::setAutomatic(bool enabled){return launch(enabled?"checks-on":"checks-off");}
bool Updates::load(){return launch("status");}
bool Updates::launch(const QString &verb){if(busy())return false;verb_=verb;output_.clear();oversized_=false;process_.start(helper_,QStringList{verb});deadline_.start(90000);if(changed)changed();return true;}
void Updates::drain(){while(process_.bytesAvailable()>0){const QByteArray data=process_.read(4096);if(output_.size()+data.size()>65536){oversized_=true;process_.kill();return;}output_.append(data);}}
}
