#pragma once
#include <QImage>
#include <QString>
#include <QWidget>
namespace tailui {
class QrWidget : public QWidget {
public:
    explicit QrWidget(QWidget *parent=nullptr);
    bool setUrl(const QString &url,int maxPixels);
    const QImage &image() const { return image_; }
    QSize sizeHint() const override { return image_.size(); }
protected:
    void paintEvent(QPaintEvent *event) override;
private:
    QString url_;
    int pixels_=-1;
    QImage image_;
};
}
