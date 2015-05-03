import socket
import sys
import pickle
import json
import numpy as np

def makePrediction(data, m, hw, num_fft):
    try:
        data = json.loads(data)
        c1 = data["Chan1"]
        c1 = c1 * hw
        c2 = data["Chan2"]
        c2 = c2 * hw
    
        fft1 = np.fft.fft(c1, num_fft)
        fft2 = np.fft.fft(c2, num_fft)

        fft1pow = np.absolute(fft1)/num_fft
        fft1pow = fft1pow[:61]
        fft2pow = np.absolute(fft2)/num_fft
        fft2pow = fft2pow[:61]

        features  = np.append(fft2pow, fft1pow)

        prediction = m.predict(features)
    except ValueError:
        print data
        return str(prediction[0])
    
    return str(prediction[0])

def main():
    num_fft = 250
    # Hanning
    hw = np.hanning(num_fft)

    # Create a TCP/IP socket
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)

    # Bind the socket to the address given on the command line
    server_address = ("", 10002)
    print >>sys.stderr, 'starting up on %s port %s' % server_address
    sock.bind(server_address)
    sock.listen(1)
    f = open('./lda_clf')

    while True:
        print >>sys.stderr, 'waiting for a connection'
        connection, client_address = sock.accept()
        try:
            m = pickle.load(f)
            print >>sys.stderr, 'client connected:', client_address
            while True:
                data = connection.recv(50000)
                prediction = makePrediction(data, m, hw, num_fft)
                connection.sendall(prediction)
        finally:
            f.close()
            connection.close()

if __name__ == '__main__':
    main()
